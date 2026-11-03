package constants

// enum:FulfillmentStatus
const (
	FulfillmentStatusPartial                  = "PARTIALLY_DISPATCHED"
	FulfillmentStatusDelivered                = "DELIVERED"
	FulfillmentStatusInvalidated              = "PLAN_INVALIDATED"
	FulfillmentStatusHeld                     = "HELD"
	FulfillmentStatusReleased                 = "RELEASED"
	FulfillmentStatusReady                    = "READY"
	FulfillmentStatusDispatching              = "DISPATCHING"
	FulfillmentStatusDispatched               = "DISPATCHED"
	FulfillmentStatusCancelRequested          = "CANCEL_REQUESTED"
	FulfillmentStatusWarehouseCancelConfirmed = "WAREHOUSE_CANCEL_CONFIRMED"
	FulfillmentStatusCancelled                = "CANCELLED"
	FulfillmentStatusInspecting               = "INSPECTING"
	FulfillmentStatusInspected                = "INSPECTED"
)

// enum:FulfillmentKind
const (
	FulfillmentKindParcel       = "PARCEL"
	FulfillmentKindCancellation = "LINE_CANCELLATION"
	FulfillmentKindGift         = "GIFT"
	FulfillmentKindOrder        = "ORDER"
	FulfillmentKindExchange     = "EXCHANGE"
	FulfillmentKindReturn       = "RETURN"
)
