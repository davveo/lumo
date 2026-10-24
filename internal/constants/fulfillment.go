package constants

// enum:FulfillmentStatus
const (
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
	FulfillmentKindOrder    = "ORDER"
	FulfillmentKindExchange = "EXCHANGE"
	FulfillmentKindReturn   = "RETURN"
)
