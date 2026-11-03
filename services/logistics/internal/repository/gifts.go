package repository

import "github.com/davveo/lumo/internal/constants"

func deliveryEventType(kind string) string {
	if kind == constants.FulfillmentKindGift {
		return constants.EventRewardDelivered
	}
	return constants.EventDeliveryConfirmed
}
