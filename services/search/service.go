package search

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/search/internal/repository"
	"github.com/davveo/lumo/services/search/internal/transport"
	"github.com/davveo/lumo/services/search/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{
		Name:    constants.ServiceSearch,
		Migrate: s.Migrate,
		Seed:    s.Seed,
		Handle:  t.Handle, Operations: t.Operations(),
		Topics:  nil,
		OnEvent: s.OnEvent,
	}
}
