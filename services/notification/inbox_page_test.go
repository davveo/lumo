package notification_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification"
	"github.com/davveo/lumo/services/notification/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"testing"
	"time"
)

func TestInboxPagesFilterAcrossHistoryKeepUnreadTotalsAndEnforceOwnership(t *testing.T) {
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "inbox.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { raw.Close() })
	m := notification.New(platform.Dependencies{DB: db})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	counts := map[string]int{}
	rows := []domain.Record{}
	events := []string{constants.EventOrderClosed, constants.EventCouponExpiring, constants.EventTicketReplied}
	for i := 0; i < 125; i++ {
		kind := events[i%3]
		counts[domain.MessageCategory(kind)]++
		payload, _ := json.Marshal(map[string]any{"event_type": kind, "title": fmt.Sprint(i)})
		state := constants.NotificationStatusRead
		if i >= 100 {
			state = constants.NotificationStatusUnread
		}
		rows = append(
			rows,
			domain.Record{
				ID:        fmt.Sprintf("message-%03d", i),
				UserID:    "buyer",
				Kind:      constants.NotificationKindInbox,
				Status:    state,
				Payload:   string(payload),
				CreatedAt: time.Now().UTC().Add(-time.Duration(i) * time.Minute),
			},
		)
	}
	rows = append(
		rows,
		domain.Record{
			ID:      "private-victim",
			UserID:  "victim",
			Kind:    constants.NotificationKindInbox,
			Status:  constants.NotificationStatusUnread,
			Payload: `{"event_type":"TicketReplied"}`,
		},
	)
	if e = db.Create(&rows).Error; e != nil {
		t.Fatal(e)
	}
	read := func(page int, filter string) platform.Result {
		t.Helper()
		out, e := m.Operations[constants.ActionList](
			ctx,
			platform.Request{
				ID:     "victim",
				UserID: "buyer",
				Role:   constants.RoleUser,
				Data:   map[string]any{"page": page, "page_size": 10, "filter": filter, "user_id": "victim"},
			},
		)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	out := read(13, constants.MessageFilterAll)
	var got struct{ Items []platform.Result }
	_ = platform.Decode(out, &got)
	if platform.Int(out, "total") != 125 || platform.Int(out, "unread_total") != 25 || len(got.Items) != 5 {
		t.Fatal(out)
	}
	for _, r := range got.Items {
		if platform.Text(r, "user_id") != "buyer" {
			t.Fatal("owner leaked", r)
		}
	}
	for category, count := range counts {
		page := read(1, category)
		if platform.Int(page, "total") != int64(count) || platform.Int(page, "unread_total") != 25 {
			t.Fatal(page)
		}
		_ = platform.Decode(page, &got)
		for _, r := range got.Items {
			if platform.Text(r, "message_type") != category {
				t.Fatal(r)
			}
		}
	}
	out = read(3, constants.MessageFilterUnread)
	_ = platform.Decode(out, &got)
	if len(got.Items) != 5 || platform.Int(out, "total") != 25 {
		t.Fatal(out)
	}
	if _, e = m.Operations[constants.ActionReadAll](ctx, platform.Request{UserID: "buyer", Role: constants.RoleUser}); e != nil {
		t.Fatal(e)
	}
	out = read(3, constants.MessageFilterUnread)
	if platform.Int(out, "page") != 1 || platform.Int(out, "total") != 0 || platform.Int(out, "unread_total") != 0 {
		t.Fatal(out)
	}
	_, e = m.Operations[constants.ActionList](
		ctx,
		platform.Request{
			UserID: "buyer",
			Role:   constants.RoleUser,
			Data:   map[string]any{"page": 1, "filter": "forged"},
		},
	)
	if status.Code(e) != codes.InvalidArgument {
		t.Fatal(e)
	}
}
