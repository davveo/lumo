package domain

import "github.com/davveo/lumo/internal/constants"

func ActivityEvents() []string {
	return []string{
		constants.EventJourneyReady,
		constants.EventProductPriceReached,
		constants.EventProductBackInStock,
		constants.EventGroupFormed,
		constants.EventGroupFailed,
		constants.EventCouponExpiring,
	}
}
func ServiceEvents() []string {
	return []string{
		constants.EventExportReady,
		constants.EventRefundSucceeded,
		constants.EventInvoiceIssued,
		constants.EventTicketReplied,
		constants.EventRedemptionIssued,
		constants.EventRedemptionRefunded,
		constants.EventRewardShipped,
		constants.EventRewardDelivered,
		constants.EventShipmentException,
	}
}
func MessageCategory(event string) string {
	for _, kind := range ActivityEvents() {
		if event == kind {
			return constants.MessageTypeActivity
		}
	}
	for _, kind := range ServiceEvents() {
		if event == kind {
			return constants.MessageTypeService
		}
	}
	return constants.MessageTypeOrder
}
