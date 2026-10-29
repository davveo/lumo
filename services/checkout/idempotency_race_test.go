package checkout_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	checkoutmod "github.com/davveo/lumo/services/checkout"
	ordermod "github.com/davveo/lumo/services/order"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type raceSubmitKey struct{}

type racingSubmitPeers struct {
	*peers
	ready   [2]chan struct{}
	release [2]chan struct{}
}

func (p *racingSubmitPeers) Call(ctx context.Context, service string, c platform.Command) (platform.Result, error) {
	if service == "pricing" {
		slot := ctx.Value(raceSubmitKey{}).(int)
		close(p.ready[slot])
		select {
		case <-p.release[slot]:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return p.peers.Call(ctx, service, c)
}

func TestConcurrentSubmitRejectsReusedKeyWithChangedPayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := &racingSubmitPeers{peers: &peers{}}
	for slot := range 2 {
		p.ready[slot], p.release[slot] = make(chan struct{}), make(chan struct{})
	}
	open := func(name string) platform.Dependencies {
		db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), name+".db"))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := db.DB()
		t.Cleanup(func() { _ = raw.Close() })
		if err = platform.MigratePlatform(ctx, db); err != nil {
			t.Fatal(err)
		}
		return platform.Dependencies{DB: db, RPC: p}
	}
	p.order = ordermod.New(open("order"))
	m := checkoutmod.New(open("checkout"))
	for _, module := range []platform.Module{p.order, m} {
		if err := module.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	type reply struct {
		data platform.Result
		err  error
	}
	responses := [2]chan reply{make(chan reply, 1), make(chan reply, 1)}
	for slot := range 2 {
		go func() {
			data, err := m.Handle(context.WithValue(ctx, raceSubmitKey{}, slot), platform.Command{
				Action: "submit", UserID: "demo", Role: "user", IdempotencyKey: "racing-key",
				Data: map[string]any{
					"quote_id": "quote-fixed", "note": []string{"first snapshot", "changed snapshot"}[slot],
					"address": map[string]any{"recipient": "Demo", "phone": "00000000000", "address": "Demo street"},
				},
			})
			responses[slot] <- reply{data, err}
		}()
		select {
		case <-p.ready[slot]:
		case <-ctx.Done():
			t.Fatal("both submits must pass the initial absent-saga read")
		}
	}
	close(p.release[0])
	first := <-responses[0]
	if first.err != nil {
		t.Fatal(first.err)
	}
	close(p.release[1])
	second := <-responses[1]
	if status.Code(second.err) != codes.AlreadyExists {
		t.Fatalf(
			"changed concurrent payload reused another snapshot: first=%v second=%v err=%v",
			first.data,
			second.data,
			second.err,
		)
	}
	if p.reserves != 1 {
		t.Fatalf("changed payload repeated reservations: %d", p.reserves)
	}
}
