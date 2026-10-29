package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/platform"
)

type Template struct {
	Title   string `json:"title"`
	Body    string `json:"body"`
	Enabled bool   `json:"enabled"`
}

func (r *Repository) Template(ctx context.Context, id string, defaults Template) (Template, int64, error) {
	return adminconfig.Read(ctx, r.db, "notification:"+id, defaults)
}
func (r *Repository) TemplateUpsert(ctx context.Context, c platform.Request, v Template) (platform.Result, error) {
	return adminconfig.Save(ctx, r.db, "notification:"+c.ID, v, platform.Int(c.Data, "version"), c.UserID, platform.Text(c.Data, "reason"))
}
