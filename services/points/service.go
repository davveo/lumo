package points

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/repository"
	"github.com/davveo/lumo/services/points/internal/transport"
	"github.com/davveo/lumo/services/points/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r}
	h := &transport.Handler{App: u}
	return platform.Module{Name: "points", Migrate: r.Migrate, Seed: r.Seed, Handle: h.Handle, Topics: []string{"RefundSucceeded", "OrderCompleted"}, OnEvent: u.OnEvent}
}
