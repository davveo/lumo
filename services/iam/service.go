package iam

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/iam/internal/repository"
	"github.com/davveo/lumo/services/iam/internal/transport"
	"github.com/davveo/lumo/services/iam/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB))
	h := transport.New(s)
	return platform.Module{
		Name:       constants.ServiceIAM,
		Migrate:    s.Migrate,
		Seed:       s.Seed,
		Handle:     h.Handle,
		Operations: h.Operations(),
	}
}
