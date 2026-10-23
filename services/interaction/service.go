package interaction

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/interaction/internal/repository"
	"github.com/davveo/lumo/services/interaction/internal/transport"
	"github.com/davveo/lumo/services/interaction/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{Name: "interaction", Migrate: s.Migrate, Seed: s.Seed, Handle: t.Handle, Topics: nil, OnEvent: s.OnEvent}
}
