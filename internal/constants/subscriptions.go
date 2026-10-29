package constants

// enum:SubscriptionKind
const (
	SubscriptionKindPrice = "price"
	SubscriptionKindStock = "stock"
)

// enum:SubscriptionStatus
const (
	SubscriptionStatusActive    = "ACTIVE"
	SubscriptionStatusTriggered = "TRIGGERED"
	SubscriptionStatusCancelled = "CANCELLED"
	SubscriptionStatusExpired   = "EXPIRED"
)
