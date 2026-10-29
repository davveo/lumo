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
	PointsLedgerKindFreeze         = "FREEZE"
	PointsLedgerKindManual         = "MANUAL"
	PointsLedgerKindConfirmed      = "CONFIRMED"
	PointsLedgerKindCancelled      = "CANCELLED"
	PointsLedgerKindReward         = "REWARD"
	PointsLedgerKindCheckin        = "CHECKIN"
	PointsLedgerKindRewardReversal = "REWARD_REVERSAL"
	PointsLedgerKindRefund         = "REFUND"
)
