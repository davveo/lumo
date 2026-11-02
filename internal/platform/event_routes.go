package platform

import "github.com/davveo/lumo/internal/constants"

// The complete topology is declared by every publisher before publishing, so
// subscribers need not have started yet. Contract tests check each module's
// independently declared topics against this deployment-wide inventory.
var eventRoutes = map[string][]string{
	constants.ServiceAftersale: {
		constants.EventAftersaleRecoveryRequested,
		constants.EventRefundSucceeded,
		constants.EventDeliveryConfirmed,
	},
	constants.ServiceAnalytics: {
		constants.EventOrderCompleted,
		constants.EventRefundSucceeded,
		constants.EventOrderPaymentAccepted,
		constants.EventPaymentSucceeded,
	},
	constants.ServiceCart: {constants.EventOrderPaymentAccepted},
	constants.ServiceCheckout: {
		constants.EventCheckoutRecoveryRequested,
		constants.EventCheckoutGroupResolution,
		constants.EventOrderExpiryRequested,
		constants.EventOrderPaymentAccepted,
		constants.EventOrderClosed,
		constants.EventGroupFormed,
		constants.EventGroupFailed,
	},
	constants.ServiceCoupon:      {constants.EventRefundSucceeded},
	constants.ServiceFlash:       {constants.EventFlashOrderRequested},
	constants.ServiceFulfillment: {constants.EventFulfillmentRecoveryRequested, constants.EventOrderReadyToFulfill},
	constants.ServiceGroup:       {constants.EventOrderPaymentAccepted},
	constants.ServiceInvoice:     {constants.EventRefundSucceeded},
	constants.ServiceMember:      {constants.EventOrderCompleted},
	constants.ServiceNotification: {
		constants.EventShipmentException,
		constants.EventProductPriceReached,
		constants.EventProductBackInStock,
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
	constants.ServiceOrder: {
		constants.EventPaymentSucceeded,
		constants.EventRefundSucceeded,
		constants.EventShipmentDispatched,
		constants.EventDeliveryConfirmed,
		constants.EventGroupFormed,
		constants.EventGroupFailed,
	},
	constants.ServicePayment: {constants.EventPaymentRejected, constants.EventGroupOrderTerminated},
	constants.ServicePoints:  {constants.EventRefundSucceeded, constants.EventOrderCompleted},
}

func EventTopics(service string) []string { return append([]string(nil), eventRoutes[service]...) }

func eventRequiresRoute(eventType string) bool {
	for _, topics := range eventRoutes {
		for _, topic := range topics {
			if topic == eventType {
				return true
			}
		}
	}
	// These informational events have a durable producer record but no current
	// subscribers. Unknown events remain mandatory, exposing routing mistakes.
	switch eventType {
	case constants.EventBehaviorAccepted,
		constants.EventProductChanged,
		constants.EventReturnInspectionCompleted,
		constants.EventReviewPublished:
		return false
	}
	return true
}
