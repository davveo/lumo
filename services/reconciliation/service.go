package reconciliation

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/reconciliation/internal/repository"
	"github.com/davveo/lumo/services/reconciliation/internal/transport"
	"github.com/davveo/lumo/services/reconciliation/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	r := repository.New(d.DB)
	u := usecase.New(r, d.RPC)
	return platform.Module{Name: constants.ServiceReconciliation, Migrate: r.Migrate, Handle: transport.New(u), Topics: u.Topics(), OnEvent: u.OnEvent, Tick: u.Tick}
}
