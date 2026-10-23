package logistics

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/logistics/internal/repository"
	"github.com/davveo/lumo/services/logistics/internal/transport"
	"github.com/davveo/lumo/services/logistics/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r, RPC: d.RPC}
	h := &transport.Handler{Service: u}
	return platform.Module{Name: "logistics", Migrate: r.Migrate, Handle: h.Handle, Topics: u.Topics(), OnEvent: u.OnEvent}
}
