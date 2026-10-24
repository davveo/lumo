package constants

// enum:CheckoutState
const (
	CheckoutStateCreating       = "CREATING"
	CheckoutStateCancelling     = "CANCELLING"
	CheckoutStatePendingPayment = "PENDING_PAYMENT"
	CheckoutStatePaidConfirming = "PAID_CONFIRMING"
	CheckoutStatePaidWaitGroup  = "PAID_WAIT_GROUP"
	CheckoutStateReadyToFulfill = "READY_TO_FULFILL"
	CheckoutStateCancelled      = "CANCELLED"
	CheckoutStateProcessing     = "PROCESSING"
)

// enum:CheckoutStepStatus
const (
	CheckoutStepStatusTrying    = "TRYING"
	CheckoutStepStatusUnknown   = "UNKNOWN"
	CheckoutStepStatusReserved  = "RESERVED"
	CheckoutStepStatusConfirmed = "CONFIRMED"
	CheckoutStepStatusCancelled = "CANCELLED"
)

// enum:CheckoutMode
const (
	CheckoutModeCreate  = "create"
	CheckoutModeRelease = "release"
	CheckoutModePaid    = "paid"
)
