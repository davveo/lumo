package constants

// enum:OutboxState
const (
	OutboxStatePending    = "PENDING"
	OutboxStatePublishing = "PUBLISHING"
	OutboxStatePublished  = "PUBLISHED"
)

// enum:InboxState
const (
	InboxStateProcessing = "PROCESSING"
	InboxStateDone       = "DONE"
	InboxStateDead       = "DEAD"
	InboxStateRetry      = "RETRY"
)

// enum:MessageExchange
const (
	MessageExchangeEvents = "lumo.events"
	MessageExchangeDead   = "lumo.dead"
)
