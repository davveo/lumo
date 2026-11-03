package constants

// enum:InteractionKind
const (
	InteractionKindModeration     = "moderation"
	InteractionKindReviewFollowup = "review_followup"
	InteractionKindReview         = "review"
	InteractionKindQuestion       = "question"
	InteractionKindLike           = "like"
)

// enum:InteractionStatus
const (
	InteractionStatusPending   = "PENDING"
	InteractionStatusPublished = "PUBLISHED"
	InteractionStatusRejected  = "REJECTED"
	InteractionStatusActive    = "ACTIVE"
)
