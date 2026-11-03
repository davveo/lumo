package constants

// enum:ExportStatus
const (
	ExportStatusQueued    = "QUEUED"
	ExportStatusRunning   = "RUNNING"
	ExportStatusUploading = "UPLOADING"
	ExportStatusReady     = "READY"
	ExportStatusFailed    = "FAILED"
)

// enum:PublicationStatus
const (
	PublicationStatusScheduled = "SCHEDULED"
	PublicationStatusSucceeded = "SUCCEEDED"
	PublicationStatusFailed    = "FAILED"
	PublicationStatusCancelled = "CANCELLED"
)

// enum:ProductDraftStatus
const (
	ProductDraftStatusDraft     = "DRAFT"
	ProductDraftStatusPublished = "PUBLISHED"
)
