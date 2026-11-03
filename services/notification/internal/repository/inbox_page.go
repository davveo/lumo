package repository

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/services/notification/internal/domain"
	"gorm.io/gorm"
)

type InboxPage struct {
	Items       []domain.Record
	Total       int64
	UnreadTotal int64
	AllTotal    int64
	Page        int
	PageSize    int
}

func (r *Repository) InboxPage(ctx context.Context, user, filter string, page, size int) (InboxPage, error) {
	out := InboxPage{Page: page, PageSize: size, Items: []domain.Record{}}
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		child := &Repository{db: tx}
		var e error
		out.AllTotal, out.UnreadTotal, e = child.Counts(ctx, user, constants.NotificationKindInbox, "")
		if e != nil {
			return e
		}
		q := child.scope(ctx, user, constants.NotificationKindInbox, "")
		event := "json_extract(payload, '$.event_type')"
		if tx.Dialector.Name() == "mysql" {
			event = "JSON_UNQUOTE(JSON_EXTRACT(payload, '$.event_type'))"
		}
		switch filter {
		case constants.MessageFilterUnread:
			q = q.Where("status = ?", constants.NotificationStatusUnread)
		case constants.MessageTypeActivity:
			q = q.Where(event+" IN ?", domain.ActivityEvents())
		case constants.MessageTypeService:
			q = q.Where(event+" IN ?", domain.ServiceEvents())
		case constants.MessageTypeOrder:
			q = q.Where("COALESCE("+event+", '') NOT IN ?", append(domain.ActivityEvents(), domain.ServiceEvents()...))
		}
		if e = q.Count(&out.Total).Error; e != nil {
			return e
		}
		last := max(1, int((out.Total+int64(size)-1)/int64(size)))
		out.Page = min(page, last)
		return q.Order("created_at DESC,id DESC").Limit(size).Offset((out.Page - 1) * size).Find(&out.Items).Error
	})
	return out, e
}
