package refund

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/refund/internal/repository"
	"github.com/davveo/lumo/services/refund/internal/transport"
	"github.com/davveo/lumo/services/refund/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := repository.New(d.DB)
	u := usecase.New(r, d.RPC)
	return platform.Module{Name: "refund", Migrate: r.Migrate, Handle: transport.New(u), Topics: u.Topics(), OnEvent: u.OnEvent, Tick: u.Tick}
}
