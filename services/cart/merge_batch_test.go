package cart_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/davveo/lumo/internal/platform"
	cartmod "github.com/davveo/lumo/services/cart"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mergeCatalog struct {
	t       *testing.T
	calls   int
	missing string
	fail    bool
}

func (p *mergeCatalog) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	p.t.Helper()
	p.calls++
	if service != "catalog" || c.Action != "batch_get" || c.UserID != "member" || c.Role != "user" {
		p.t.Fatalf("unexpected RPC: %s %#v", service, c)
	}
	if p.fail {
		return nil, status.Error(codes.Unavailable, "catalog unavailable")
	}
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := platform.Decode(c.Data, &in); err != nil {
		return nil, err
	}
	rows := []platform.Result{}
	for _, id := range in.IDs {
		if id != p.missing {
			rows = append(rows, platform.Result{"id": id})
		}
	}
	return platform.Result{"items": rows}, nil
}
func TestMergeBatchesValidationBeforeAnyCartWrite(t *testing.T) {
	for _, n := range []int{1, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			ctx := context.Background()
			db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "cart.db"))
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := db.DB()
			t.Cleanup(func() { _ = raw.Close() })
			rpc := &mergeCatalog{t: t}
			m := cartmod.New(platform.Dependencies{DB: db, RPC: rpc})
			if migrateErr := platform.MigratePlatform(ctx, db); migrateErr != nil {
				t.Fatal(migrateErr)
			}
			if e = m.Migrate(ctx); e != nil {
				t.Fatal(e)
			}
			items := []map[string]any{}
			for i := 0; i < n; i++ {
				items = append(items, map[string]any{"sku_id": fmt.Sprintf("sku-%03d", i), "quantity": 2})
			}
			req := platform.Request{
				UserID:         "member",
				Role:           "user",
				IdempotencyKey: "merge",
				Data:           map[string]any{"items": items},
			}
			count := func() int64 {
				var n int64
				if e := db.Table("cart_records").Count(&n).Error; e != nil {
					t.Fatal(e)
				}
				return n
			}
			rpc.missing = fmt.Sprintf("sku-%03d", n-1)
			if _, e = m.Operations["merge"](ctx, req); status.Code(e) != codes.NotFound || count() != 0 ||
				rpc.calls != 1 {
				t.Fatalf("partial validation wrote cart: %v calls=%d", e, rpc.calls)
			}
			rpc.missing = ""
			rpc.fail = true
			if _, e = m.Operations["merge"](ctx, req); status.Code(e) != codes.Unavailable || count() != 0 {
				t.Fatalf("failed bulk read wrote cart: %v", e)
			}
			rpc.fail = false
			rpc.calls = 0
			if _, e = m.Operations["merge"](ctx, req); e != nil || count() != int64(n) || rpc.calls != 1 {
				t.Fatalf("merge n=%d count=%d calls=%d error=%v", n, count(), rpc.calls, e)
			}
			if _, e = m.Operations["merge"](ctx, req); e != nil || count() != int64(n) {
				t.Fatalf("replay duplicated cart: %v", e)
			}
			rpc.calls = 0
			items[0]["quantity"] = 11
			if _, e = m.Operations["merge"](ctx, req); status.Code(e) != codes.InvalidArgument || rpc.calls != 0 {
				t.Fatalf("invalid local input caused RPC: %v", e)
			}
		})
	}
}
