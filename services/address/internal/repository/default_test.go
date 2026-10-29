package repository_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/address/internal/domain"
	"github.com/davveo/lumo/services/address/internal/repository"
)

func TestStaleAddressEditCannotRestorePreviousDefault(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "address.db"))
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { _ = sql.Close() })
	r := repository.New(db)
	if err = r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		row := domain.Record{ID: id, UserID: "buyer", Kind: constants.AddressKindAddress, Payload: `{}`}
		if err = r.Save(ctx, &row, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = r.SetDefault(ctx, "first", "buyer"); err != nil {
		t.Fatal(err)
	}
	// Update loads its address before another request changes the default.
	stale, err := r.Get(ctx, "first")
	if err != nil || !stale.IsDefault {
		t.Fatalf("read default address: %+v %v", stale, err)
	}
	if err = r.SetDefault(ctx, "second", "buyer"); err != nil {
		t.Fatal(err)
	}
	stale.Payload = `{"address":"edited street"}`
	if err = r.Save(ctx, &stale, nil); err != nil {
		t.Fatal(err)
	}
	if stale.IsDefault || stale.Payload != `{"address":"edited street"}` {
		t.Fatalf("content save restored obsolete default: %+v", stale)
	}
	rows, err := r.List(ctx, "buyer", constants.AddressKindAddress, "", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	defaults := 0
	for _, row := range rows {
		if row.IsDefault {
			defaults++
			if row.ID != "second" {
				t.Fatalf("wrong default after delayed edit: %+v", row)
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("default count=%d, want 1", defaults)
	}
}
