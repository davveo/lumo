package points

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/repository"
	"github.com/davveo/lumo/services/points/internal/transport"
	"github.com/davveo/lumo/services/points/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r}
	h := transport.New(u)
	return platform.Module{
		Name:       constants.ServicePoints,
		Migrate:    r.Migrate,
		Seed:       r.Seed,
		Handle:     h.Handle,
		Operations: h.Operations(),
		Topics:     []string{constants.EventRefundSucceeded, constants.EventOrderCompleted},
		OnEvent:    u.OnEvent,
	}
}
