package constants

// enum:OrderStatus
const (
	OrderStatusCreating          = "CREATING"
	OrderStatusPendingPayment    = "PENDING_PAYMENT"
	OrderStatusPaidConfirming    = "PAID_CONFIRMING"
	OrderStatusPaidWaitGroup     = "PAID_WAIT_GROUP"
	OrderStatusReadyToFulfill    = "READY_TO_FULFILL"
	OrderStatusShipped           = "SHIPPED"
	OrderStatusCompleted         = "COMPLETED"
	OrderStatusClosing           = "CLOSING"
	OrderStatusClosed            = "CLOSED"
	OrderStatusCreateFailed      = "CREATE_FAILED"
	OrderStatusTerminated        = "TERMINATED"
	OrderStatusCancelled         = "CANCELLED"
	OrderStatusPartiallyRefunded = "PARTIALLY_REFUNDED"
	OrderStatusRefunded          = "REFUNDED"
	OrderStatusPaid              = "PAID"
	OrderStatusAwaitingGroup     = "AWAITING_GROUP"
	OrderStatusWaitingGroup      = "WAITING_GROUP"
	OrderStatusFulfilling        = "FULFILLING"
	OrderStatusCreated           = "CREATED"
	OrderStatusAwaitingPayment   = "AWAITING_PAYMENT"
	OrderStatusDelivered         = "DELIVERED"
)

// enum:OrderSource
const (
	OrderSourceCart   = "cart"
	OrderSourceDirect = "direct"
)
