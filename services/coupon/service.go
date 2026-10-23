package coupon

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/repository"
	"github.com/davveo/lumo/services/coupon/internal/transport"
	"github.com/davveo/lumo/services/coupon/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r}
	h := &transport.Handler{App: u}
	return platform.Module{Name: "coupon", Migrate: r.Migrate, Seed: r.Seed, Handle: h.Handle, Topics: []string{"RefundSucceeded"}, OnEvent: u.OnEvent}
}
