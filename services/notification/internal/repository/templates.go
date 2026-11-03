package repository

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"strings"
)

type Template struct {
	EventType string `json:"event_type,omitempty"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Enabled   bool   `json:"enabled"`
}

func (r *Repository) Template(ctx context.Context, id string, defaults Template) (Template, int64, error) {
	binding, _, e := adminconfig.Read(ctx, r.db, "notification-binding:"+id, map[string]string{})
	if e != nil {
		return defaults, 0, e
	}
	if target := binding["template_id"]; target != "" {
		return adminconfig.Read(ctx, r.db, "notification-template:"+target, defaults)
	}
	return r.BaseTemplate(ctx, id, defaults)
}
func (r *Repository) TemplateUpsert(ctx context.Context, c platform.Request, v Template) (platform.Result, error) {
	var result platform.Result
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		key := "notification:" + c.ID
		event := c.ID
		target := ""
		custom := v.EventType != ""
		if custom {
			key = "notification-template:" + c.ID
			event = v.EventType
			target = c.ID
			var count int64
			if e := tx.Model(&adminconfig.Document{}).Where("id LIKE ?", "notification-template:%").Count(&count).Error; e != nil {
				return e
			}
			if count >= 500 && platform.Int(c.Data, "version") == 0 {
				return status.Error(codes.ResourceExhausted, "template library is limited to 500 entries")
			}
		}
		if custom {
			old, version, e := adminconfig.Read(ctx, tx, key, Template{})
			if e != nil {
				return e
			}
			if version > 0 && old.EventType != event {
				return status.Error(codes.FailedPrecondition, "template event cannot change; create a new template")
			}
		}
		var e error
		result, e = adminconfig.Save(
			ctx,
			tx,
			key,
			v,
			platform.Int(c.Data, "version"),
			c.UserID,
			platform.Text(c.Data, "reason"),
		)
		if e != nil {
			return e
		}
		binding, _, e := adminconfig.Read(ctx, tx, "notification-binding:"+event, map[string]string{})
		if e != nil {
			return e
		}
		if !custom || c.Data["bind"] == true {
			return r.saveBinding(ctx, tx, event, target, c)
		}
		if binding["template_id"] == c.ID {
			return r.saveBinding(ctx, tx, event, "", c)
		}
		return nil
	})
	return result, err
}

// A binding selects one reusable template for a known event. Drafts are not
// delivered until the operator explicitly binds them.
func (r *Repository) Bindings(ctx context.Context) (map[string]string, error) {
	var rows []adminconfig.Document
	if e := r.db.WithContext(ctx).Where("id LIKE ?", "notification-binding:%").Find(&rows).Error; e != nil {
		return nil, e
	}
	out := map[string]string{}
	for _, row := range rows {
		var v struct {
			ID string `json:"template_id"`
		}
		if e := json.Unmarshal([]byte(row.Payload), &v); e != nil {
			return nil, e
		}
		out[strings.TrimPrefix(row.ID, "notification-binding:")] = v.ID
	}
	return out, nil
}

type TemplateRow struct {
	ID      string
	Version int64
	Value   Template
}

func (r *Repository) CustomTemplates(ctx context.Context) ([]TemplateRow, error) {
	var docs []adminconfig.Document
	if e := r.db.WithContext(ctx).Where("id LIKE ?", "notification-template:%").Order("id").Limit(501).Find(&docs).Error; e != nil {
		return nil, e
	}
	rows := []TemplateRow{}
	for _, doc := range docs {
		var v Template
		if e := json.Unmarshal([]byte(doc.Payload), &v); e != nil {
			return nil, e
		}
		rows = append(rows, TemplateRow{strings.TrimPrefix(doc.ID, "notification-template:"), doc.Version, v})
	}
	return rows, nil
}
func (r *Repository) BaseTemplate(ctx context.Context, id string, defaults Template) (Template, int64, error) {
	return adminconfig.Read(ctx, r.db, "notification:"+id, defaults)
}
func (r *Repository) saveBinding(ctx context.Context, tx *gorm.DB, event, id string, c platform.Request) error {
	key := "notification-binding:" + event
	_, version, e := adminconfig.Read(ctx, tx, key, map[string]string{})
	if e != nil {
		return e
	}
	_, e = adminconfig.Save(
		ctx,
		tx,
		key,
		map[string]string{"template_id": id},
		version,
		c.UserID,
		platform.Text(c.Data, "reason"),
	)
	return e
}
