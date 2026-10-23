package analytics

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/analytics/internal/repository"
	"github.com/davveo/lumo/services/analytics/internal/transport"
	"github.com/davveo/lumo/services/analytics/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{Name: "analytics", Migrate: s.Migrate, Seed: s.Seed, Handle: t.Handle, Topics: []string{"OrderCompleted", "RefundSucceeded"}, OnEvent: s.OnEvent}
}
