package helpdesk

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk/internal/repository"
	"github.com/davveo/lumo/services/helpdesk/internal/transport"
	"github.com/davveo/lumo/services/helpdesk/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{Name: "helpdesk", Migrate: s.Migrate, Seed: s.Seed, Handle: t.Handle, Topics: nil, OnEvent: s.OnEvent}
}
