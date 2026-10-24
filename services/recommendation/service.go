package recommendation

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/recommendation/internal/repository"
	"github.com/davveo/lumo/services/recommendation/internal/transport"
	"github.com/davveo/lumo/services/recommendation/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{Name: constants.ServiceRecommendation, Migrate: s.Migrate, Seed: s.Seed, Handle: t.Handle, Topics: nil, OnEvent: s.OnEvent}
}
