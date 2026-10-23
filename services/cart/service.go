package cart

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/cart/internal/repository"
	"github.com/davveo/lumo/services/cart/internal/transport"
	"github.com/davveo/lumo/services/cart/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{Name: "cart", Migrate: s.Migrate, Seed: s.Seed, Handle: t.Handle, Topics: []string{"OrderPaymentAccepted"}, OnEvent: s.OnEvent}
}
