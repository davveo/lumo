package member_test

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/member"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"testing"
)

type customerPeer struct{}

func (customerPeer) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	return platform.Result{"id": c.ID, "role": "user"}, nil
}
func TestCustomerCardsAndSegmentsValidateOwnershipAndVersions(t *testing.T) {
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "customer.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	m := member.New(platform.Dependencies{DB: db, RPC: customerPeer{}})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	req := platform.Request{
		ID:     "buyer",
		UserID: "operator",
		Role:   "admin",
		Data:   map[string]any{"tags": []string{"老客", "老客"}, "note": "接续服务", "version": 0},
	}
	out, e := m.Operations["customer_update"](ctx, req)
	if e != nil || platform.Int(out, "version") != 1 {
		t.Fatal(out, e)
	}
	if _, e = m.Operations["customer_update"](ctx, req); status.Code(e) != codes.Aborted {
		t.Fatal(e)
	}
	req.Role = "user"
	if _, e = m.Operations["customer_update"](ctx, req); status.Code(e) != codes.PermissionDenied {
		t.Fatal(e)
	}
	out, e = m.Operations["customer_get"](ctx, platform.Request{ID: "other", UserID: "operator", Role: "admin"})
	if e != nil || platform.Int(out, "version") != 0 || platform.Text(out, "note") != "" {
		t.Fatal(out, e)
	}
	rule := platform.Request{
		ID:     "segment-one",
		UserID: "operator",
		Role:   "admin",
		Data: map[string]any{
			"name":          "老客",
			"min_orders":    2,
			"min_net_cents": 10000,
			"inactive_days": 30,
			"version":       0,
		},
	}
	out, e = m.Operations["segment_upsert"](ctx, rule)
	if e != nil || platform.Int(out, "version") != 1 {
		t.Fatal(out, e)
	}
	if _, e = m.Operations["segment_upsert"](ctx, rule); status.Code(e) != codes.Aborted {
		t.Fatal(e)
	}
}
