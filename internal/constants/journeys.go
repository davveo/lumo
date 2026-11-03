package constants

// enum:JourneyKind
const (
	JourneyKindCart       = "cart"
	JourneyKindUnpaid     = "unpaid"
	JourneyKindRepurchase = "repurchase"
)

// enum:JourneyStatus
const (
	JourneyStatusWatching   = "WATCHING"
	JourneyStatusQueued     = "QUEUED"
	JourneyStatusDelivered  = "DELIVERED"
	JourneyStatusSuppressed = "SUPPRESSED"
	JourneyStatusExited     = "EXITED"
	JourneyStatusRecovered  = "RECOVERED"
)
