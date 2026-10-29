package engagement_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCompareLimitIsAtomicAcrossServiceInstances(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "engagement.db") + "?_busy_timeout=10000&_journal_mode=WAL"
	modules := make([]platform.Module, 2)
	for i := range modules {
		db, err := platform.OpenDB(ctx, "sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		sql, _ := db.DB()
		t.Cleanup(func() { _ = sql.Close() })
		modules[i] = engagement.New(platform.Dependencies{DB: db, RPC: recencyCatalog{}})
		if err = modules[i].Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	put := func(module platform.Module, user, sku string) error {
		_, err := module.Operations["put"](ctx, platform.Request{UserID: user, Role: "user", Data: map[string]any{
			"kind": "compare", "sku_id": sku,
		}})
		return err
	}
	if err := put(modules[0], "buyer", "existing-a"); err != nil {
		t.Fatal(err)
	}
	if err := put(modules[1], "buyer", "existing-b"); err != nil {
		t.Fatal(err)
	}
	const workers = 16
	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- put(modules[i%2], "buyer", fmt.Sprintf("racing-%d", i))
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("unexpected concurrent compare error: %v", err)
		}
	}
	if accepted != 1 {
		t.Fatalf("new comparisons accepted=%d, want only the last available slot", accepted)
	}
	if err := put(modules[1], "buyer", "existing-a"); err != nil {
		t.Fatalf("refreshing an existing comparison at capacity: %v", err)
	}
	if err := put(modules[0], "other", "independent"); err != nil {
		t.Fatalf("another owner shares the limit: %v", err)
	}
	for _, check := range []struct {
		user  string
		total int64
	}{{"buyer", 3}, {"other", 1}} {
		list, err := modules[0].Operations["list"](
			ctx,
			platform.Request{UserID: check.user, Role: "user", Data: map[string]any{"kind": "compare"}},
		)
		if err != nil || platform.Int(list, "total") != check.total {
			t.Fatalf("compare list for %s: %+v %v", check.user, list, err)
		}
		favorites, err := modules[0].Operations["list"](
			ctx,
			platform.Request{UserID: check.user, Role: "user", Data: map[string]any{"kind": "favorite"}},
		)
		if err != nil || platform.Int(favorites, "total") != 0 {
			t.Fatalf("owner guards appeared as business records: %+v %v", favorites, err)
		}
	}
}
