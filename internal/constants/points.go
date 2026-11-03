package constants

// enum:PointsHoldState
const (
	PointsHoldStateReserved  = "RESERVED"
	PointsHoldStateConfirmed = "CONFIRMED"
	PointsHoldStateCancelled = "CANCELLED"
	PointsHoldStateRefunded  = "REFUNDED"
)

// enum:PointsLedgerKind
const (
	PointsLedgerKindRedemption         = "REDEMPTION"
	PointsLedgerKindRedemptionReversal = "REDEMPTION_REVERSAL"
	PointsLedgerKindFreeze             = "FREEZE"
	PointsLedgerKindManual             = "MANUAL"
	PointsLedgerKindConfirmed          = "CONFIRMED"
	PointsLedgerKindCancelled          = "CANCELLED"
	PointsLedgerKindReward             = "REWARD"
	PointsLedgerKindCheckin            = "CHECKIN"
	PointsLedgerKindRewardReversal     = "REWARD_REVERSAL"
	PointsLedgerKindRefund             = "REFUND"
)

// enum:RedemptionState
const (
	RedemptionStatePending    = "PENDING"
	RedemptionStateProcessing = "PROCESSING"
	RedemptionStateIssued     = "ISSUED"
	RedemptionStateShipped    = "SHIPPED"
	RedemptionStateDelivered  = "DELIVERED"
	RedemptionStateRefunded   = "REFUNDED"
	RedemptionStateAttention  = "ATTENTION"
)

// enum:RewardKind
const (
	RewardKindCoupon = "COUPON"
	RewardKindGift   = "GIFT"
)
