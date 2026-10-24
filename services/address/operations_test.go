package address_test

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/address"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"testing"
)

func TestIndependentAddressMethodsPreserveDefaultAndOwnership(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "address.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	module := address.New(platform.Dependencies{DB: db})
	if err = module.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	request := platform.Request{
		UserID: "buyer",
		Role:   "user",
		Data: map[string]any{
			"recipient":  "buyer",
			"phone":      "13800000000",
			"address":    "street one",
			"is_default": true,
		},
	}
	created, err := module.Operations["create"](ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.ID = platform.Text(created, "id")
	request.Data = map[string]any{"recipient": "buyer", "phone": "13800000000", "address": "street two"}
	updated, err := module.Operations["update"](ctx, request)
	if err != nil || updated["address"] != "street two" || updated["is_default"] != true {
		t.Fatalf("update lost default or data: %v %v", updated, err)
	}
	request.UserID = "another-user"
	if _, err = module.Operations["update"](ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("update accepted another owner's record: %v", err)
	}
	request.UserID = "buyer"
	got, err := module.Operations["get"](ctx, request)
	if err != nil || got["address"] != "street two" {
		t.Fatalf("unauthorized update changed record: %v %v", got, err)
	}
	if _, err = module.Operations["delete"](ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err = module.Operations["get"](ctx, request); status.Code(err) != codes.NotFound {
		t.Fatalf("delete did not remove address: %v", err)
	}
}
