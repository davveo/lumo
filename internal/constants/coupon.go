package constants

// enum:CouponStatus
const (
	CouponStatusAvailable       = "AVAILABLE"
	CouponStatusHeld            = "HELD"
	CouponStatusUsed            = "USED"
	CouponStatusExpired         = "EXPIRED"
	CouponStatusAvailableLegacy = "available"
)

// enum:CouponHoldState
const (
	CouponHoldStateReserved  = "RESERVED"
	CouponHoldStateConfirmed = "CONFIRMED"
	CouponHoldStateCancelled = "CANCELLED"
	CouponHoldStateRefunded  = "REFUNDED"
)

// enum:CouponClaimStatus
const (
	CouponClaimStatusAvailable       = "available"
	CouponClaimStatusUnavailable     = "unavailable"
	CouponClaimStatusClaimed         = "claimed"
	CouponClaimStatusEnded           = "ended"
	CouponClaimStatusUpcoming        = "upcoming"
	CouponClaimStatusExhausted       = "exhausted"
	CouponClaimStatusLoginRequired   = "login_required"
	CouponClaimStatusConditionsUnmet = "conditions_unmet"
)

// enum:CouponRefundStatus
const (
	CouponRefundStatusNotEligible     = "NOT_ELIGIBLE"
	CouponRefundStatusReturned        = "RETURNED"
	CouponRefundStatusNoCoupon        = "NO_COUPON"
	CouponRefundStatusExpiredNoReturn = "EXPIRED_NO_RETURN"
)

// enum:CouponRefundPolicy
const (
	CouponRefundPolicyFullRefundOnly = "full_refund_only"
)

// enum:CouponLedgerKind
const (
	CouponLedgerKindConfirmed = "CONFIRMED"
	CouponLedgerKindCancelled = "CANCELLED"
	CouponLedgerKindAvailable = "AVAILABLE"
	CouponLedgerKindExpired   = "EXPIRED"
)

// enum:CouponType
const (
	CouponTypeReduction         = "reduction"
	CouponTypeCash              = "cash"
	CouponTypeDiscount          = "discount"
	CouponTypeThresholdDiscount = "threshold_discount"
)

// enum:CouponScope
const (
	CouponScopeAll      = "all"
	CouponScopeCategory = "category"
	CouponScopeProduct  = "product"
)
