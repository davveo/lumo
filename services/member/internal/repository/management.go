package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/platform"
)

func (r *Repository) Policy(ctx context.Context) (commercepolicy.Membership, error) {
	p, v, e := adminconfig.Read(ctx, r.db, "membership-policy", commercepolicy.DefaultMembership())
	p.Version = v
	return p, e
}
func (r *Repository) PolicyUpsert(ctx context.Context, c platform.Request, p commercepolicy.Membership) (platform.Result, error) {
	return adminconfig.Save(ctx, r.db, "membership-policy", p, platform.Int(c.Data, "version"), c.UserID, platform.Text(c.Data, "reason"))
}
