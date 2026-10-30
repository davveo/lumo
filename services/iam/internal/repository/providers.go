package repository

import (
	"context"
	"github.com/davveo/lumo/internal/channelconfig"
	"github.com/davveo/lumo/internal/platform"
)

func (r *Repository) Provider(ctx context.Context, id string, defaults map[string]string) (channelconfig.Profile, error) {
	return channelconfig.Read(ctx, r.db, "login-provider-"+id, defaults)
}
func (r *Repository) SaveProvider(ctx context.Context, schema channelconfig.Schema, p channelconfig.Profile, c platform.Request) (platform.Result, error) {
	return channelconfig.Save(ctx, r.db, "login-provider-"+schema.ID, schema, p, c)
}
