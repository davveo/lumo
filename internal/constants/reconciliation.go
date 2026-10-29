package constants

// enum:ReconciliationStatus
const (
	ReconciliationStatusMatched     = "MATCHED"
	ReconciliationStatusQueued      = "QUEUED"
	ReconciliationStatusRunning     = "RUNNING"
	ReconciliationStatusFailed      = "FAILED"
	ReconciliationStatusDifferences = "DIFFERENCES"
)

// enum:ReconciliationDifferenceKind
const (
	ReconciliationDifferenceKindOrderUnavailable                 = "ORDER_UNAVAILABLE"
	ReconciliationDifferenceKindAmountMismatch                   = "AMOUNT_MISMATCH"
	ReconciliationDifferenceKindPaymentNotAcceptedOrPendingEvent = "PAYMENT_NOT_ACCEPTED_OR_PENDING_EVENT"
)
