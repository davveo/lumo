package coupon

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/coupon/internal/repository"
	"github.com/davveo/lumo/services/coupon/internal/transport"
	"github.com/davveo/lumo/services/coupon/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r, RPC: d.RPC}
	h := transport.New(u)
	return platform.Module{
		Name:       constants.ServiceCoupon,
		Migrate:    r.Migrate,
		Seed:       r.Seed,
		Handle:     h.Handle,
		Operations: h.Operations(),
		Topics:     []string{constants.EventRefundSucceeded},
		OnEvent:    u.OnEvent,
	}
}
