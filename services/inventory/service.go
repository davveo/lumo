package inventory

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/repository"
	"github.com/davveo/lumo/services/inventory/internal/transport"
	"github.com/davveo/lumo/services/inventory/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r}
	h := &transport.Handler{App: u}
	return platform.Module{Name: "inventory", Migrate: r.Migrate, Seed: r.Seed, Handle: h.Handle}
}
