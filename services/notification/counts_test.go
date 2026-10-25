package notification_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification"
	"github.com/davveo/lumo/services/notification/internal/domain"
	"github.com/davveo/lumo/services/notification/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInboxReportsExactOwnedUnreadCountBeyondThePageLimit(t *testing.T) {
	ctx := context.Background()
	db, err := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "counts.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	module := notification.New(platform.Dependencies{DB: db})
	if err = module.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	records := []domain.Record{}
	for _, fixture := range []struct {
		owner, kind, state string
		count              int
	}{
		{"buyer", constants.NotificationKindInbox, constants.NotificationStatusUnread, 123},
		{"buyer", constants.NotificationKindInbox, constants.NotificationStatusRead, 7},
		{"buyer", "other-kind", constants.NotificationStatusUnread, 5},
		{"other", constants.NotificationKindInbox, constants.NotificationStatusUnread, 137},
		{"other", constants.NotificationKindInbox, constants.NotificationStatusRead, 3},
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
	assertList := func(owner, role string, limit, offset int, total, unread, items int64) {
		t.Helper()
		result, err := module.Operations[constants.ActionList](ctx, platform.Request{
			UserID: owner, Role: role,
			Data: map[string]any{
				"limit": limit, "offset": offset,
				"user_id": "forged-user", "kind": "other-kind", "status": constants.NotificationStatusRead,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			Items       []platform.Result `json:"items"`
			Total       int64             `json:"total"`
			UnreadTotal int64             `json:"unread_total"`
			Limit       int64             `json:"limit"`
		}
		if err = platform.Decode(result, &response); err != nil {
			t.Fatal(err)
		}
		if response.Total != total || response.UnreadTotal != unread || int64(len(response.Items)) != items ||
			response.Limit != items {
			t.Fatalf("inbox counts inherited pagination, another owner or another kind: %#v", result)
		}
		for _, item := range response.Items {
			if item["user_id"] != owner || item["kind"] != constants.NotificationKindInbox {
				t.Fatal("returned a row outside the counters' scope", item)
			}
		}
	}
	assertList("buyer", "user", 100, 0, 130, 123, 100)
	assertList("buyer", "user", 1, 129, 130, 123, 1)
	assertList("buyer", "user", 100, 130, 130, 123, 0)
	assertList("buyer", "user", 1000, 0, 130, 123, 20)
	assertList("other", "admin", 100, 0, 140, 137, 100)
	assertList("empty", "user", 100, 0, 0, 0, 0)
	if _, err := module.Operations[constants.ActionList](ctx, platform.Request{}); status.Code(
		err,
	) != codes.Unauthenticated {
		t.Fatal("inbox counts were available without a principal", err)
	}
	repo := repository.New(db)
	for _, fixture := range []struct {
		state         string
		total, unread int64
	}{
		{constants.NotificationStatusRead, 7, 0},
		{constants.NotificationStatusUnread, 123, 123},
	} {
		total, unread, err := repo.Counts(ctx, "buyer", constants.NotificationKindInbox, fixture.state)
		if err != nil || total != fixture.total || unread != fixture.unread {
			t.Fatal("status filter did not apply consistently to both SQL counts", total, unread, err)
		}
	}
	_, err = module.Operations[constants.ActionRead](ctx, platform.Request{
		ID:     fmt.Sprintf("buyer-%s-%s-000", constants.NotificationKindInbox, constants.NotificationStatusUnread),
		UserID: "buyer", Role: "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertList("buyer", "user", 100, 0, 130, 122, 100)
	marked, err := module.Operations[constants.ActionReadAll](ctx, platform.Request{UserID: "buyer", Role: "user"})
	if err != nil || platform.Int(marked, "marked_read") != 122 {
		t.Fatal("read-all did not preserve the same unread scope", marked, err)
	}
	assertList("buyer", "user", 100, 0, 130, 0, 100)
	assertList("other", "user", 100, 0, 140, 137, 100)
}
