package checkout

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/checkout/internal/repository"
	"github.com/davveo/lumo/services/checkout/internal/transport"
	"github.com/davveo/lumo/services/checkout/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := repository.New(d.DB)
	u := usecase.New(r, d.RPC)
	return platform.Module{Name: constants.ServiceCheckout, Migrate: r.Migrate, Handle: transport.New(u), Topics: u.Topics(), OnEvent: u.OnEvent, Tick: u.Tick}
}
