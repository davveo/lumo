package inventory

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/repository"
	"github.com/davveo/lumo/services/inventory/internal/transport"
	"github.com/davveo/lumo/services/inventory/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r}
	t := transport.New(u)
	return platform.Module{
		Name:       constants.ServiceInventory,
		Migrate:    r.Migrate,
		Seed:       r.Seed,
		Handle:     t.Handle,
		Operations: t.Operations(),
	}
}
