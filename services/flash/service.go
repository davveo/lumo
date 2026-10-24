package flash

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/flash/internal/repository"
	"github.com/davveo/lumo/services/flash/internal/transport"
	"github.com/davveo/lumo/services/flash/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r, RPC: d.RPC, Redis: d.Redis}
	h := &transport.Handler{App: u}
	return platform.Module{
		Name:    constants.ServiceFlash,
		Migrate: r.Migrate,
		Seed:    r.Seed,
		Handle:  h.Handle,
		Topics:  []string{constants.EventFlashOrderRequested},
		OnEvent: u.OnEvent,
	}
}
