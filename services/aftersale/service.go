package aftersale

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/repository"
	"github.com/davveo/lumo/services/aftersale/internal/transport"
	"github.com/davveo/lumo/services/aftersale/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r, RPC: d.RPC}
	h := &transport.Handler{Service: u}
	return platform.Module{Name: "aftersale", Migrate: r.Migrate, Handle: h.Handle, Topics: u.Topics(), OnEvent: u.OnEvent, Tick: u.Tick}
}
