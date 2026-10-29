package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/platform"
)

func (r *Repository) Policy(ctx context.Context) (commercepolicy.Shipping, error) {
	p, v, e := adminconfig.Read(ctx, r.DB, "shipping-policy", commercepolicy.DefaultShipping())
	p.Version = v
	return p, e
}
func (r *Repository) PolicyUpsert(ctx context.Context, c platform.Request, p commercepolicy.Shipping) (platform.Result, error) {
	return adminconfig.Save(ctx, r.DB, "shipping-policy", p, platform.Int(c.Data, "version"), c.UserID, platform.Text(c.Data, "reason"))
}
