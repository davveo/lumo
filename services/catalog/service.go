package catalog

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/repository"
	"github.com/davveo/lumo/services/catalog/internal/transport"
	"github.com/davveo/lumo/services/catalog/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{
		Name:    constants.ServiceCatalog,
		Migrate: s.Migrate,
		Seed:    s.Seed,
		Handle:  t.Handle,
		Topics:  nil,
		OnEvent: s.OnEvent,
	}
}
