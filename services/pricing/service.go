package pricing

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/pricing/internal/repository"
	"github.com/davveo/lumo/services/pricing/internal/transport"
	"github.com/davveo/lumo/services/pricing/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := repository.New(d.DB)
	u := usecase.New(r, d.RPC)
	t := transport.New(u)
	return platform.Module{
		Name:       constants.ServicePricing,
		Migrate:    r.Migrate,
		Handle:     t.Handle,
		Operations: t.Operations(),
		Topics:     u.Topics(),
		OnEvent:    u.OnEvent,
		Tick:       u.Tick,
	}
}
