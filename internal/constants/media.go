package constants

// enum:MediaKind
const (
	MediaKindMetadata = "metadata"
)

// enum:MediaStatus
const (
	MediaStatusRegistered = "REGISTERED"
	MediaStatusUploading  = "UPLOADING"
	MediaStatusReady      = "READY"
	MediaStatusRejected   = "REJECTED"
)

// enum:MediaStorageMode
const (
	MediaStorageModeMetadataOnly = "metadata_only"
	MediaStorageModeMinio        = "minio"
)

// enum:MediaContentType
const (
	MediaContentTypeJPEG      = "image/jpeg"
	MediaContentTypePNG       = "image/png"
	MediaContentTypeWebP      = "image/webp"
	MediaContentTypePDF       = "application/pdf"
	MediaContentTypePlainText = "text/plain"
)
