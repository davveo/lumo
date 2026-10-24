package constants

// enum:InventoryReservationState
const (
	InventoryReservationStateReserved   = "RESERVED"
	InventoryReservationStateAllocated  = "ALLOCATED"
	InventoryReservationStateCancelled  = "CANCELLED"
	InventoryReservationStateDispatched = "DISPATCHED"
)

// enum:InventoryMovementKind
const (
	InventoryMovementKindReserve    = "RESERVE"
	InventoryMovementKindAllocated  = "ALLOCATED"
	InventoryMovementKindCancelled  = "CANCELLED"
	InventoryMovementKindDispatched = "DISPATCHED"
)

// enum:StockCondition
const (
	StockConditionSellable   = "sellable"
	StockConditionDamaged    = "damaged"
	StockConditionUnsellable = "unsellable"
)
