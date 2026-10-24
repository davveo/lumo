package constants

// enum:AftersaleStatus
const (
	AftersaleStatusRequested         = "REQUESTED"
	AftersaleStatusApproved          = "APPROVED"
	AftersaleStatusRejected          = "REJECTED"
	AftersaleStatusCancelled         = "CANCELLED"
	AftersaleStatusReturning         = "RETURNING"
	AftersaleStatusInspecting        = "INSPECTING"
	AftersaleStatusInspected         = "INSPECTED"
	AftersaleStatusRefundRequested   = "REFUND_REQUESTED"
	AftersaleStatusRefunding         = "REFUNDING"
	AftersaleStatusExchangePreparing = "EXCHANGE_PREPARING"
	AftersaleStatusExchanging        = "EXCHANGING"
	AftersaleStatusCompleted         = "COMPLETED"
	AftersaleStatusExchanged         = "EXCHANGED"
)

// enum:AftersaleType
const (
	AftersaleTypeRefund       = "refund"
	AftersaleTypeReturn       = "return"
	AftersaleTypeExchange     = "exchange"
	AftersaleTypeRefundOnly   = "refund_only"
	AftersaleTypeReturnRefund = "return_refund"
)
