package analytics

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/repository"
	"github.com/davveo/lumo/services/analytics/internal/transport"
	"github.com/davveo/lumo/services/analytics/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{
		Name:    constants.ServiceAnalytics,
		Migrate: s.Migrate,
		Seed:    s.Seed,
		Handle:  t.Handle,
		Topics:  []string{constants.EventOrderCompleted, constants.EventRefundSucceeded},
		OnEvent: s.OnEvent,
	}
}
