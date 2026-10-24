package engagement

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement/internal/repository"
	"github.com/davveo/lumo/services/engagement/internal/transport"
	"github.com/davveo/lumo/services/engagement/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{Name: constants.ServiceEngagement, Migrate: s.Migrate, Seed: s.Seed, Handle: t.Handle, Topics: nil, OnEvent: s.OnEvent}
}
