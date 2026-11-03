package tests_test

import (
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification"
	"testing"
)

func TestMySQLNotificationPaginationUsesScopedCategoryFilters(t *testing.T) {
	ctx, db := adminMySQLScratch(t, "inbox_pages")
	if e := platform.MigratePlatform(ctx, db); e != nil {
		t.Fatal(e)
	}
	m := notification.New(platform.Dependencies{DB: db})
	if e := m.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 12; i++ {
		if e := m.OnEvent(ctx, platform.Event{ID: fmt.Sprintf("inbox-mysql-%d", i), Type: constants.EventTicketReplied, AggregateID: fmt.Sprint(i), UserID: "buyer"}); e != nil {
			t.Fatal(e)
		}
	}
	out, e := m.Operations[constants.ActionList](
		ctx,
		platform.Request{
			UserID: "buyer",
			Role:   constants.RoleUser,
			Data:   map[string]any{"page": 2, "page_size": 10, "filter": constants.MessageTypeService},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	var page struct{ Items []platform.Result }
	_ = platform.Decode(out, &page)
	if len(page.Items) != 2 || platform.Int(out, "total") != 12 || platform.Int(out, "unread_total") != 12 {
		t.Fatal(out)
	}
}
