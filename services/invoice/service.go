package invoice

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/invoice/internal/repository"
	"github.com/davveo/lumo/services/invoice/internal/transport"
	"github.com/davveo/lumo/services/invoice/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := &repository.Repository{DB: d.DB}
	u := &usecase.Service{Repo: r, RPC: d.RPC}
	t := transport.New(u)
	return platform.Module{
		Name:       constants.ServiceInvoice,
		Migrate:    r.Migrate,
		Handle:     t.Handle,
		Operations: t.Operations(),
		Topics:     u.Topics(),
		OnEvent:    u.OnEvent,
	}
}
