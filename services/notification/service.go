package notification

import (
	"github.com/davveo/lumo/internal/constants"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification/internal/repository"
	"github.com/davveo/lumo/services/notification/internal/transport"
	"github.com/davveo/lumo/services/notification/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{
		Name:    constants.ServiceNotification,
		Migrate: s.Migrate,
		Seed:    s.Seed,
		Handle:  t.Handle,
		Topics: []string{
			constants.EventOrderAwaitingPayment,
			constants.EventOrderPaymentAccepted,
			constants.EventOrderClosed,
			constants.EventGroupFormed,
			constants.EventGroupFailed,
			constants.EventShipmentDispatched,
			constants.EventDeliveryConfirmed,
			constants.EventOrderCompleted,
			constants.EventRefundSucceeded,
			constants.EventInvoiceIssued,
			constants.EventTicketReplied,
		},
		OnEvent: s.OnEvent,
	}
}
