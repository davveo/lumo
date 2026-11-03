package notification_test

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"testing"
)

func TestReusableTemplatesRequireExplicitKnownEventBindingAndPreserveHistory(t *testing.T) {
	ctx := context.Background()
	db, e := platform.OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "templates.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	if e = platform.MigratePlatform(ctx, db); e != nil {
		t.Fatal(e)
	}
	m := notification.New(platform.Dependencies{DB: db})
	if e = m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	save := func(id, event, title string, bind bool, version int64) (platform.Result, error) {
		return m.Operations["template_upsert"](
			ctx,
			platform.Request{
				ID:     id,
				UserID: "admin",
				Role:   "admin",
				Data: map[string]any{
					"event_type": event,
					"title":      title,
					"body":       "订单 {{business_id}}",
					"enabled":    true,
					"bind":       bind,
					"version":    version,
					"reason":     "测试业务通知",
				},
			},
		)
	}
	if _, e = save("custom-notice", "UnsupportedEvent", "无效", false, 0); status.Code(e) != codes.InvalidArgument {
		t.Fatalf("unknown event accepted %v", e)
	}
	if _, e = save("custom-notice", constants.EventOrderCompleted, "备用标题", false, 0); e != nil {
		t.Fatal(e)
	}
	project := func(id string) {
		t.Helper()
		if e = m.OnEvent(ctx, platform.Event{ID: id, Type: constants.EventOrderCompleted, AggregateID: "order", UserID: "buyer"}); e != nil {
			t.Fatal(e)
		}
	}
	project("before-binding")
	if _, e = save("custom-notice", constants.EventOrderCompleted, "自定义完成标题", true, 1); e != nil {
		t.Fatal(e)
	}
	project("after-binding")
	rows, e := m.Operations["list"](ctx, platform.Request{UserID: "buyer", Role: "user"})
	if e != nil {
		t.Fatal(e)
	}
	var values struct {
		Items []platform.Result `json:"items"`
	}
	_ = platform.Decode(rows, &values)
	titles := map[string]bool{}
	for _, row := range values.Items {
		titles[platform.Text(row, "title")] = true
	}
	if !titles["订单已完成"] || !titles["自定义完成标题"] {
		t.Fatal(rows)
	}
	if _, e = save("custom-notice", constants.EventOrderCompleted, "过期覆盖", true, 1); status.Code(e) != codes.Aborted {
		t.Fatalf("stale version accepted %v", e)
	}
	if _, e = save("custom-notice", constants.EventDeliveryConfirmed, "错换事件", true, 2); status.Code(
		e,
	) != codes.FailedPrecondition {
		t.Fatalf("event changed %v", e)
	}
	if _, e = save("custom-notice", constants.EventOrderCompleted, "备用标题", false, 2); e != nil {
		t.Fatal(e)
	}
	list, e := m.Operations["templates_admin"](ctx, platform.Request{UserID: "admin", Role: "admin"})
	if e != nil {
		t.Fatal(e)
	}
	var all struct{ Items []platform.Result }
	_ = platform.Decode(list, &all)
	found := false
	for _, row := range all.Items {
		if platform.Text(row, "id") == "custom-notice" {
			found = true
			if row["bound"] == true {
				t.Fatal("unbound template active", row)
			}
		}
	}
	if !found {
		t.Fatal(list)
	}
}
