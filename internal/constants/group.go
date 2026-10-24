package constants

// enum:GroupStatus
const (
	GroupStatusOpen      = "OPEN"
	GroupStatusFormed    = "FORMED"
	GroupStatusFailed    = "FAILED"
	GroupStatusExpired   = "EXPIRED"
	GroupStatusCancelled = "CANCELLED"
)

// enum:GroupStatusLegacy
const (
	GroupStatusLegacyFormed = "formed"
	GroupStatusLegacyFailed = "failed"
)

// enum:GroupSeatState
const (
	GroupSeatStateReserved  = "RESERVED"
	GroupSeatStateCancelled = "CANCELLED"
	GroupSeatStatePaid      = "PAID"
	GroupSeatStateLatePaid  = "LATE_PAID"
)
