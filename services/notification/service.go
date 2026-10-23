package notification

import (
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification/internal/repository"
	"github.com/davveo/lumo/services/notification/internal/transport"
	"github.com/davveo/lumo/services/notification/internal/usecase"
)

func New(d platform.Dependencies) platform.Module {
	s := usecase.New(repository.New(d.DB), d.RPC)
	t := transport.New(s)
	return platform.Module{Name: "notification", Migrate: s.Migrate, Seed: s.Seed, Handle: t.Handle, Topics: []string{"OrderAwaitingPayment", "OrderPaymentAccepted", "OrderClosed", "GroupFormed", "GroupFailed", "ShipmentDispatched", "DeliveryConfirmed", "OrderCompleted", "RefundSucceeded", "InvoiceIssued", "TicketReplied"}, OnEvent: s.OnEvent}
}
