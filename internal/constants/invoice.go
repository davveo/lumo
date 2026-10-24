package constants

// enum:InvoiceStatus
const (
	InvoiceStatusRequested       = "REQUESTED"
	InvoiceStatusIssued          = "ISSUED"
	InvoiceStatusReversalPending = "REVERSAL_PENDING"
	InvoiceStatusCancelled       = "CANCELLED"
	InvoiceStatusAdjustedRequest = "ADJUSTED_REQUEST"
	InvoiceStatusAdjusted        = "ADJUSTED"
	InvoiceStatusVoid            = "VOID"
)

// enum:InvoiceReversalStatus
const (
	InvoiceReversalStatusCancelledUnissued         = "CANCELLED_UNISSUED"
	InvoiceReversalStatusPendingExternalAdjustment = "PENDING_EXTERNAL_ADJUSTMENT"
	InvoiceReversalStatusDemoAdjusted              = "DEMO_ADJUSTED"
)

// enum:InvoiceKind
const (
	InvoiceKindPersonal = "personal"
	InvoiceKindCompany  = "company"
)

// enum:InvoiceProvider
const (
	InvoiceProviderUnsubmitted   = "unsubmitted"
	InvoiceProviderLocalDemoOnly = "local-demo-only"
)
