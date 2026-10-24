package refund

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/refund/internal/repository"
	"github.com/davveo/lumo/services/refund/internal/transport"
	"github.com/davveo/lumo/services/refund/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := repository.New(d.DB)
	u := usecase.New(r, d.RPC)
	t := transport.New(u)
	return platform.Module{
		Name:       constants.ServiceRefund,
		Migrate:    r.Migrate,
		Handle:     t.Handle,
		Operations: t.Operations(),
		Topics:     u.Topics(),
		OnEvent:    u.OnEvent,
		Tick:       u.Tick,
	}
}
