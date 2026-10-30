package repository

import (
	"context"
	"github.com/davveo/lumo/internal/channelconfig"
	"github.com/davveo/lumo/internal/platform"
)

func (r *Repository) Provider(ctx context.Context, id string) (channelconfig.Profile, error) {
	p, e := channelconfig.Read(ctx, r.db, "payment-provider-"+id, map[string]string{"environment": "production"})
	if p.Version == 0 {
		p.Source = "defaults"
	}
	return p, e
}
func (r *Repository) SaveProvider(ctx context.Context, schema channelconfig.Schema, p channelconfig.Profile, c platform.Request) (platform.Result, error) {
	return channelconfig.Save(ctx, r.db, "payment-provider-"+schema.ID, schema, p, c)
}
