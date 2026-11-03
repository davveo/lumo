package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/platform"
)

func (r *Repository) Policy(ctx context.Context) (commercepolicy.Aftersale, error) {
	p, version, e := adminconfig.Read(ctx, r.DB, "aftersale-policy", commercepolicy.DefaultAftersale())
	p.Version = version
	return p, e
}

func (r *Repository) PolicyUpsert(
	ctx context.Context,
	c platform.Request,
	p commercepolicy.Aftersale,
) (platform.Result, error) {
	return adminconfig.Save(
		ctx,
		r.DB,
		"aftersale-policy",
		p,
		platform.Int(c.Data, "version"),
		c.UserID,
		platform.Text(c.Data, "reason"),
	)
}
