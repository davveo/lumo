package order

import (
	"github.com/davveo/lumo/internal/orderwire"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/repository"
	"github.com/davveo/lumo/services/order/internal/transport"
	"github.com/davveo/lumo/services/order/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := repository.New(d.DB)
	u := usecase.New(r, d.RPC)
	return platform.Module{Name: "order", Migrate: r.Migrate, Handle: transport.New(u), RegisterRPC: transport.Register(u), RPCActions: orderwire.Actions(), Topics: u.Topics(), OnEvent: u.OnEvent, Tick: u.Tick}
}
