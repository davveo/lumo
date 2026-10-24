package constants

// enum:CampaignKind
const (
	CampaignKindAddon            = "addon"
	CampaignKindFlash            = "flash"
	CampaignKindGroup            = "group"
	CampaignKindReduction        = "reduction"
	CampaignKindDiscount         = "discount"
	CampaignKindQuantityDiscount = "quantity_discount"
	CampaignKindDirect           = "direct"
)

// enum:CampaignHoldState
const (
	CampaignHoldStateReserved  = "RESERVED"
	CampaignHoldStateConfirmed = "CONFIRMED"
	CampaignHoldStateCancelled = "CANCELLED"
)
