package constants

// Frontend projections deliberately have their own names: their lowercase
// values are view models, rather than wire statuses of the owning services.

// enum:OrderViewStatus
const (
	OrderViewStatusPending   = "pending"
	OrderViewStatusShipping  = "shipping"
	OrderViewStatusReceiving = "receiving"
	OrderViewStatusReview    = "review"
	OrderViewStatusCompleted = "completed"
	OrderViewStatusRefunded  = "refunded"
	OrderViewStatusGroup     = "group"
	OrderViewStatusClosed    = "closed"
)

// enum:CouponViewStatus
const (
	CouponViewStatusAvailable = "available"
	CouponViewStatusExpired   = "expired"
	CouponViewStatusUsed      = "used"
	CouponViewStatusFrozen    = "frozen"
)

// enum:AftersaleViewStatus
const (
	AftersaleViewStatusProcessing = "processing"
	AftersaleViewStatusApproved   = "approved"
	AftersaleViewStatusReturning  = "returning"
	AftersaleViewStatusCompleted  = "completed"
	AftersaleViewStatusRejected   = "rejected"
	AftersaleViewStatusCancelled  = "cancelled"
)

// enum:PromotionType
const (
	PromotionTypeFlash    = "flash"
	PromotionTypeGroup    = "group"
	PromotionTypeExchange = "exchange"
)

// enum:PromotionOfferStatus
const (
	PromotionOfferStatusActive   = "active"
	PromotionOfferStatusEnded    = "ended"
	PromotionOfferStatusUpcoming = "upcoming"
)

// enum:GroupViewStatus
const (
	GroupViewStatusWaiting = "waiting"
	GroupViewStatusFormed  = "formed"
	GroupViewStatusExpired = "expired"
)

// enum:PublicationViewStatus
const (
	PublicationViewStatusRejected   = "rejected"
	PublicationViewStatusModerating = "moderating"
	PublicationViewStatusAnswered   = "answered"
	PublicationViewStatusPending    = "pending"
)

// enum:InvoiceViewStatus
const (
	InvoiceViewStatusIssued     = "issued"
	InvoiceViewStatusProcessing = "processing"
	InvoiceViewStatusVoid       = "void"
	InvoiceViewStatusAvailable  = "available"
)

// enum:TicketViewStatus
const (
	TicketViewStatusOpen     = "open"
	TicketViewStatusAnswered = "answered"
	TicketViewStatusClosed   = "closed"
)

// enum:ConversationRole
const (
	ConversationRoleUser      = "user"
	ConversationRoleAssistant = "assistant"
)

// enum:MessageType
const (
	MessageTypeOrder    = "order"
	MessageTypeActivity = "activity"
	MessageTypeService  = "service"
)

// enum:CouponFilter
const (
	CouponFilterAll         = "all"
	CouponFilterTimed       = "timed"
	CouponFilterConditional = "conditional"
)

// enum:CatalogViewStatus
const (
	CatalogViewStatusAll = "all"
	CatalogViewStatusOn  = "on"
	CatalogViewStatusOff = "off"
)

// enum:OrderDataSource
const OrderDataSourceBackend = "backend"

// enum:InvoiceEditorMode
const (
	InvoiceEditorModeCheckout = "checkout"
	InvoiceEditorModeOrder    = "order"
)

// enum:MessageFilter
const (
	MessageFilterAll    = "all"
	MessageFilterUnread = "unread"
)

// enum:ProductPool
const (
	ProductPoolSearch = "search"
	ProductPoolRecent = "recent"
)

// enum:GroupPurchaseMode
const (
	GroupPurchaseModeCreate = "create"
	GroupPurchaseModeJoin   = "join"
)

// enum:PromotionSession
const (
	PromotionSessionToday    = "today"
	PromotionSessionTomorrow = "tomorrow"
)

// enum:ReviewFilter
const (
	ReviewFilterAll  = "all"
	ReviewFilterGood = "good"
	ReviewFilterLow  = "low"
	ReviewFilterMine = "mine"
)

// enum:AuthBroadcastType
const AuthBroadcastTypeLogout = "logout"

// enum:ApiErrorCode
const (
	ApiErrorCodeSessionChanged = "SessionChanged"
	ApiErrorCodeNotFound       = "NotFound"
)
