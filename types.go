package polipage

import (
	"github.com/poli-page/sdk-go/internal/clientconfig"
)

// PageFormat is a canonical Poli Page page format. Values match the
// strings accepted by the deployed API and shared across every SDK.
type PageFormat string

// Canonical page formats. See the platform spec for the authoritative list.
const (
	PageFormatA3        PageFormat = "A3"
	PageFormatA4        PageFormat = "A4"
	PageFormatA5        PageFormat = "A5"
	PageFormatA6        PageFormat = "A6"
	PageFormatB4        PageFormat = "B4"
	PageFormatB5        PageFormat = "B5"
	PageFormatLetter    PageFormat = "Letter"
	PageFormatLegal     PageFormat = "Legal"
	PageFormatTabloid   PageFormat = "Tabloid"
	PageFormatExecutive PageFormat = "Executive"
	PageFormatStatement PageFormat = "Statement"
	PageFormatFolio     PageFormat = "Folio"
)

// Orientation is the page orientation: portrait or landscape.
type Orientation string

// Canonical orientations.
const (
	OrientationPortrait  Orientation = "portrait"
	OrientationLandscape Orientation = "landscape"
)

// RenderMetadata is free-form caller metadata forwarded to the API and
// echoed on responses that support it. Values must be primitives (string,
// number, bool); nested maps and slices are rejected at serialise time
// with *Error{Code: ErrCodeInvalidOptions}.
type RenderMetadata = map[string]any

// RenderInput is the input of every render method: a template of a
// project, rendered with the project's copy published in the API key's
// environment. Only these seven fields are sent; unset optional fields are
// omitted from the wire.
type RenderInput struct {
	// Required.
	Project  string         `json:"project"`
	Template string         `json:"template"`
	Data     map[string]any `json:"data"`

	// Optional — omitted from the wire when unset.
	Format      PageFormat     `json:"format,omitempty"`
	Orientation Orientation    `json:"orientation,omitempty"`
	Locale      string         `json:"locale,omitempty"`
	Metadata    RenderMetadata `json:"metadata,omitempty"`
}

// RetryEvent is delivered to the OnRetry hook before each retry sleep.
// Aliased to [clientconfig.RetryEvent] so option.WithOnRetry and the
// re-export here can both spell the type identically.
type RetryEvent = clientconfig.RetryEvent

// DocumentPreviewResult is the result of [Documents.Preview] — the
// paginated HTML for an already-stored document and the page count the
// engine emitted at render time.
type DocumentPreviewResult struct {
	HTML      string
	PageCount int
}

// ThumbnailFormat is the image format for a generated thumbnail. The API
// default is JPEG, which also accepts a Quality option.
type ThumbnailFormat string

// Canonical thumbnail formats.
const (
	ThumbnailFormatJPEG ThumbnailFormat = "jpeg"
	ThumbnailFormatWebP ThumbnailFormat = "webp"
	ThumbnailFormatPNG  ThumbnailFormat = "png"
)

// ThumbnailOptions configures a thumbnail-generation request. Every field
// is optional: a zero value is not sent and the API applies its default.
// The SDK does not check bounds; the API answers 400 VALIDATION_ERROR.
type ThumbnailOptions struct {
	// Width is the thumbnail width in pixels, 50-2000. API default: 400.
	Width int `json:"width,omitempty"`
	// Format selects jpeg, webp or png. API default: jpeg.
	Format ThumbnailFormat `json:"format,omitempty"`
	// Quality is the JPEG quality 1-100. Only valid when Format is JPEG.
	Quality int `json:"quality,omitempty"`
	// Pages selects specific 1-based page numbers. Empty means all pages.
	Pages []int `json:"pages,omitempty"`
}

// Thumbnail is a single page thumbnail returned by [Documents.Thumbnails].
// Data is base64-encoded image bytes; the caller decodes via
// encoding/base64.StdEncoding.DecodeString as needed.
type Thumbnail struct {
	Page        int    `json:"page"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"contentType"`
	Data        string `json:"data"`
}

// DocumentDescriptor is a stored document returned by [Render.Document]
// and [Documents.Get]. Top-level fields are system-controlled; Metadata
// echoes caller-supplied data.
//
// Nullable wire fields (ProjectID, ProjectSlug, TemplateID, TemplateSlug,
// APIKeyID, Orientation, Locale) are *string so the SDK can distinguish
// JSON null from an empty string.
type DocumentDescriptor struct {
	DocumentID     string `json:"documentId"`
	OrganizationID string `json:"organizationId"`

	ProjectID    *string `json:"projectId"`
	ProjectSlug  *string `json:"projectSlug"`
	TemplateID   *string `json:"templateId"`
	TemplateSlug *string `json:"templateSlug"`
	APIKeyID     *string `json:"apiKeyId"`

	// Environment is the name of the environment the document was rendered
	// in ("sandbox", "live", or any other environment of the organization),
	// returned as the API sent it.
	Environment string     `json:"environment"`
	Format      PageFormat `json:"format"`

	Orientation *string `json:"orientation"`
	Locale      *string `json:"locale"`

	PageCount int   `json:"pageCount"`
	SizeBytes int64 `json:"sizeBytes"`

	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`

	// Metadata is the caller-supplied metadata echoed by the server. Always
	// non-nil after a successful decode — a server null is normalised to an
	// empty map so callers can range without a nil check.
	Metadata RenderMetadata `json:"metadata"`

	// PresignedPDFURL has a ~15-minute TTL. Re-fetch via Documents.Get when
	// expired.
	PresignedPDFURL string `json:"presignedPdfUrl"`

	// client is the parent SDK client used to fetch the presigned PDF URL.
	// Set by the SDK after JSON-decode; nil for descriptors built outside
	// of a Render.Document / Documents.Get call.
	client *Client
}
