package group

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/group/internal/repository"
	"github.com/davveo/lumo/services/group/internal/transport"
	"github.com/davveo/lumo/services/group/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r, RPC: d.RPC}
	h := transport.New(u)
	return platform.Module{
		Name:       constants.ServiceGroup,
		Migrate:    r.Migrate,
		Seed:       r.Seed,
		Handle:     h.Handle,
		Operations: h.Operations(),
		Topics:     []string{constants.EventOrderPaymentAccepted},
		OnEvent:    u.OnEvent,
		Tick:       u.Tick,
	}
}
