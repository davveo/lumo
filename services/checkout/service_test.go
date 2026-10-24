package checkout_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/davveo/lumo/internal/platform"
	checkoutmod "github.com/davveo/lumo/services/checkout"
	ordermod "github.com/davveo/lumo/services/order"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"testing"
	"time"
)

type peers struct {
	order          platform.Module
	failReserve    bool
	held           bool
	cancelled      bool
	reserves       int
	quoteItems     []platform.Result
	catalogRows    map[string]platform.Result
	catalogError   error
	catalogBatches int
	catalogGets    int
	catalogIDs     []string
	catalogUser    string
	catalogRole    string
}

func (p *peers) Call(ctx context.Context, name string, c platform.Command) (platform.Result, error) {
	switch name {
	case "pricing":
		items := p.quoteItems
		if items == nil {
			items = []platform.Result{{"sku_id": "sku-test", "quantity": int64(1), "unit_price_cents": int64(1000)}}
		}
		var total int64
		for _, item := range items {
			total += platform.Int(item, "quantity") * platform.Int(item, "unit_price_cents")
		}
		return platform.Result{"quote_id": "quote-fixed", "user_id": "demo", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), "payable_cents": total, "subtotal_cents": total, "items": items}, nil
	case "catalog":
		if c.Action == "batch_get" {
			p.catalogBatches++
			p.catalogUser, p.catalogRole = c.UserID, c.Role
			var data struct {
				IDs []string `json:"ids"`
			}
			if err := platform.Decode(c.Data, &data); err != nil {
				return nil, err
			}
			p.catalogIDs = data.IDs
			if p.catalogError != nil {
				return nil, p.catalogError
			}
			rows := p.catalogRows
			if rows == nil {
				rows = map[string]platform.Result{"sku-test": {"id": "sku-test", "price_cents": int64(1000), "active": true}}
			}
			items, missing := []platform.Result{}, []string{}
			for _, id := range data.IDs {
				if row, ok := rows[id]; ok {
					items = append(items, row)
				} else {
					missing = append(missing, id)
				}
			}
			return platform.Result{"items": items, "missing_ids": missing}, nil
		}
		p.catalogGets++
		return platform.Result{"id": "sku-test", "price_cents": int64(1000), "active": true}, nil
	case "order":
		return p.order.Handle(ctx, c)
	case "inventory":
		switch c.Action {
		case "reserve":
			p.reserves++
			p.held = true
			if p.failReserve {
				return nil, status.Error(codes.DeadlineExceeded, "response lost after successful reserve")
			}
			return platform.Result{"status": "RESERVED"}, nil
		case "cancel":
			p.held = false
			p.cancelled = true
			return platform.Result{"status": "CANCELLED"}, nil
		}
	}
	return nil, status.Error(codes.Unimplemented, "unexpected test RPC")
}
func setup(t *testing.T) (platform.Module, *peers) {
	t.Helper()
	ctx := context.Background()
	p := &peers{}
	open := func(name string) platform.Dependencies {
		db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), name+".db"))
		if e != nil {
			t.Fatal(e)
		}
		sql, _ := db.DB()
		t.Cleanup(func() { _ = sql.Close() })
		if e = platform.MigratePlatform(ctx, db); e != nil {
			t.Fatal(e)
		}
		return platform.Dependencies{DB: db, RPC: p}
	}
	p.order = ordermod.New(open("order"))
	if e := p.order.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	m := checkoutmod.New(open("checkout"))
	if e := m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	return m, p
}
func TestUnknownReservationIsCompensated(t *testing.T) {
	m, p := setup(t)
	p.failReserve = true
	ctx := context.Background()
	c := platform.Command{Action: "submit", UserID: "demo", Role: "user", IdempotencyKey: "lost-response", Data: map[string]any{"quote_id": "quote-fixed", "address": map[string]any{"recipient": "Demo", "phone": "00000000000", "address": "Demo street"}}}
	_, e := m.Handle(ctx, c)
	if status.Code(e) != codes.DeadlineExceeded {
		t.Fatalf("wrong failure: %v", e)
	}
	if p.held || !p.cancelled {
		t.Fatal("UNKNOWN reservation was leaked instead of cancelled")
	}
	h := sha256.Sum256([]byte("demo\x00lost-response"))
	id := "ord_" + hex.EncodeToString(h[:])[:32]
	v, e := m.Handle(ctx, platform.Command{Action: "get", ID: id, UserID: "demo", Role: "user"})
	if e != nil || platform.Text(v, "status") != "CANCELLED" {
		t.Fatalf("compensation state not durable: %v %v", v, e)
	}
	o, e := p.order.Handle(ctx, platform.Command{Action: "get", ID: id, UserID: "demo", Role: "user"})
	if e != nil || platform.Text(o, "status") != "CREATE_FAILED" {
		t.Fatalf("failed order left payable: %v %v", o, e)
	}
}
func TestSubmitIdempotencyPreservesSnapshot(t *testing.T) {
	m, p := setup(t)
	ctx := context.Background()
	c := platform.Command{Action: "submit", UserID: "demo", Role: "user", IdempotencyKey: "same-request", Data: map[string]any{"quote_id": "quote-fixed", "address": map[string]any{"recipient": "Demo", "phone": "00000000000", "address": "Demo street"}}}
	first, e := m.Handle(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	second, e := m.Handle(ctx, c)
	if e != nil || first["order_id"] != second["order_id"] || p.reserves != 1 {
		t.Fatalf("duplicate request reran resource mutation: %#v %#v %v", first, second, e)
	}
	c.Data["address"] = map[string]any{"recipient": "Different", "phone": "00000000000", "address": "Different street"}
	_, e = m.Handle(ctx, c)
	if status.Code(e) != codes.AlreadyExists {
		t.Fatalf("reused key silently accepted changed address: %v", e)
	}
}

func TestMissingAddressRejectedBeforeReservation(t *testing.T) {
	m, p := setup(t)
	_, e := m.Handle(context.Background(), platform.Command{Action: "submit", UserID: "demo", Role: "user", IdempotencyKey: "no-address", Data: map[string]any{"quote_id": "quote-fixed"}})
	if status.Code(e) != codes.InvalidArgument || p.reserves != 0 {
		t.Fatalf("missing address accepted: %v reserves=%d", e, p.reserves)
	}
}
