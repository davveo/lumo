package engagement_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement"
	"github.com/davveo/lumo/services/engagement/internal/domain"
	"github.com/davveo/lumo/services/engagement/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListReportsExactOwnedKindTotalBeyondThePageLimit(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "counts.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	module := engagement.New(platform.Dependencies{DB: db})
	if err = module.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	records := []domain.Record{}
	for _, fixture := range []struct {
		owner, kind, state string
		count              int
	}{
		{"buyer", constants.EngagementKindFavorite, constants.EngagementStatusActive, 123},
		{"buyer", constants.EngagementKindFavorite, "archived-fixture", 2},
		{"buyer", constants.EngagementKindHistory, constants.EngagementStatusActive, 7},
		{"other", constants.EngagementKindFavorite, constants.EngagementStatusActive, 137},
	} {
		for index := 0; index < fixture.count; index++ {
			records = append(records, domain.Record{
				ID:        fmt.Sprintf("%s-%s-%s-%03d", fixture.owner, fixture.kind, fixture.state, index),
				UserID:    fixture.owner,
				Kind:      fixture.kind,
				Status:    fixture.state,
				Payload:   `{}`,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			})
		}
	}
	if err = db.Create(&records).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, owner, role, kind string
		limit, offset           int
		total, items            int64
	}{
		{"first bounded page", "buyer", "user", "favorite", 100, 0, 125, 100},
		{"second page", "buyer", "user", "favorite", 100, 100, 125, 25},
		{"single row", "buyer", "user", "favorite", 1, 100, 125, 1},
		{"past last page", "buyer", "user", "favorite", 100, 125, 125, 0},
		{"default kind and limit", "buyer", "user", "", 0, 0, 125, 20},
		{"oversized request remains bounded", "buyer", "user", "favorite", 1000, 0, 125, 20},
		{"other kind", "buyer", "user", "history", 100, 0, 7, 7},
		{"other account", "other", "user", "favorite", 100, 0, 137, 100},
		{"admin still reads own account", "other", "admin", "favorite", 100, 0, 137, 100},
		{"empty account", "empty", "user", "favorite", 100, 0, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := module.Operations[constants.ActionList](ctx, platform.Request{
				UserID: test.owner, Role: test.role,
				Data: map[string]any{
					"kind": test.kind, "limit": test.limit, "offset": test.offset,
					"user_id": "forged-user", "role": "admin",
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			var response struct {
				Items []platform.Result `json:"items"`
				Total int64             `json:"total"`
				Limit int64             `json:"limit"`
			}
			if err = platform.Decode(result, &response); err != nil {
				t.Fatal(err)
			}
			if response.Total != test.total || int64(len(response.Items)) != test.items ||
				response.Limit != test.items {
				t.Fatalf("count was limited, offset or borrowed from another scope: %#v", result)
			}
			kind := test.kind
			if kind == "" {
				kind = constants.EngagementKindFavorite
			}
			for _, item := range response.Items {
				if item["user_id"] != test.owner || item["kind"] != kind {
					t.Fatalf("returned a row outside the total's scope: %#v", item)
				}
			}
		})
	}
	if _, err := module.Operations[constants.ActionList](ctx, platform.Request{}); status.Code(
		err,
	) != codes.Unauthenticated {
		t.Fatal("count was available without a principal", err)
	}
	// The repository shares the exact same optional status filter for lists and
	// counts, including documents excluded by a narrower caller-owned read.
	repo := repository.New(db)
	total, err := repo.Count(ctx, "buyer", constants.EngagementKindFavorite, constants.EngagementStatusActive)
	if err != nil || total != 123 {
		t.Fatal("status filter did not apply to the SQL count", total, err)
	}
}
