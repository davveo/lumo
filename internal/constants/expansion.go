package constants

// enum:CatalogImportStatus
const (
	CatalogImportStatusPreview          = "PREVIEW"
	CatalogImportStatusImporting        = "IMPORTING"
	CatalogImportStatusCompleted        = "COMPLETED"
	CatalogImportStatusPartialFailed    = "PARTIAL_FAILED"
	CatalogImportStatusValidationFailed = "VALIDATION_FAILED"
)

// enum:CatalogImportRowStatus
const (
	CatalogImportRowStatusValid     = "VALID"
	CatalogImportRowStatusInvalid   = "INVALID"
	CatalogImportRowStatusSucceeded = "SUCCEEDED"
	CatalogImportRowStatusFailed    = "FAILED"
)

// enum:PurchaseStatus
const (
	PurchaseStatusDraft             = "DRAFT"
	PurchaseStatusApproved          = "APPROVED"
	PurchaseStatusPartiallyReceived = "PARTIALLY_RECEIVED"
	PurchaseStatusReceived          = "RECEIVED"
	PurchaseStatusCancelled         = "CANCELLED"
)

// enum:PurchaseMovement
const (
	PurchaseMovementReceipt = "PURCHASE_RECEIPT"
	PurchaseMovementDamaged = "PURCHASE_DAMAGED"
)

// enum:TrackingStatus
const (
	TrackingStatusCreated        = "CREATED"
	TrackingStatusPickedUp       = "PICKED_UP"
	TrackingStatusInTransit      = "IN_TRANSIT"
	TrackingStatusOutForDelivery = "OUT_FOR_DELIVERY"
	TrackingStatusDelivered      = "DELIVERED"
	TrackingStatusException      = "EXCEPTION"
)

// enum:DeliveryExceptionStatus
const (
	DeliveryExceptionStatusOpen     = "OPEN"
	DeliveryExceptionStatusResolved = "RESOLVED"
)
