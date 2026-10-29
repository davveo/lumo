package constants

// enum:ContentKind
const (
	ContentKindFaq                = "faq"
	ContentKindNotice             = "notice"
	ContentKindStorefront         = "storefront"
	ContentKindCategoryNavigation = "category_navigation"
	ContentKindCategories         = "categories"
)

// enum:ContentStatus
const (
	ContentStatusPublished = "PUBLISHED"
	ContentStatusDraft     = "DRAFT"
	ContentStatusScheduled = "SCHEDULED"
	ContentStatusPaused    = "PAUSED"
)

// enum:PopupStatus
const (
	PopupStatusDraft     = "DRAFT"
	PopupStatusPublished = "PUBLISHED"
	PopupStatusPaused    = "PAUSED"
)

// enum:PopupSegment
const (
	PopupSegmentAll             = "all"
	PopupSegmentGuest           = "guest"
	PopupSegmentNewGuest        = "new_guest"
	PopupSegmentReturningGuest  = "returning_guest"
	PopupSegmentMember          = "member"
	PopupSegmentNewMember       = "new_member"
	PopupSegmentReturningMember = "returning_member"
	PopupSegmentGoldMember      = "gold_member"
)

// enum:PopupPlacement
const (
	PopupPlacementHome     = "home"
	PopupPlacementCategory = "category"
	PopupPlacementProduct  = "product"
	PopupPlacementCoupons  = "coupons"
	PopupPlacementNone     = "none"
)

// enum:PopupAction
const (
	PopupActionClaimCoupon  = "claim_coupon"
	PopupActionCouponCenter = "coupon_center"
	PopupActionLink         = "link"
)

// enum:PopupEvent
const (
	PopupEventImpression = "impression"
	PopupEventClick      = "click"
	PopupEventDismiss    = "dismiss"
	PopupEventAutoClose  = "auto_close"
)

// enum:PopupTheme
const (
	PopupThemeRed   = "red"
	PopupThemePeach = "peach"
	PopupThemePlum  = "plum"
)

// enum:PopupLayout
const (
	PopupLayoutCoupon       = "coupon"
	PopupLayoutAnnouncement = "announcement"
	PopupLayoutImage        = "image"
	PopupLayoutWelcome      = "welcome"
	PopupLayoutMember       = "member"
	PopupLayoutCampaign     = "campaign"
)
