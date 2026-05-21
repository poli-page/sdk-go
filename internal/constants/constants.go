// Package constants holds the SDK-internal string values shared between
// the transport layer and the namespace implementations: API path templates,
// canonical HTTP header names, and any other magic strings that would
// otherwise be re-typed at multiple call sites.
//
// User-facing defaults (DefaultBaseURL, DefaultMaxRetries, etc.) live at
// the root polipage package per sdk-go-plan.md §17.
package constants

import (
	"net/url"
	"time"
)

// RetryAfterCap is the upper bound applied to any Retry-After value the SDK
// honors. Server-suggested delays beyond this are clamped to keep retry
// loops bounded under pathological back-pressure responses. Mirrors
// RETRY_AFTER_CAP_MS in the Node SDK (src/internal/http.ts:1).
const RetryAfterCap = 30 * time.Second

// Jitter bounds for ComputeBackoff: the multiplier applied to the
// exponential delay is uniform in [JitterMin, JitterMax).
const (
	JitterMin = 0.5
	JitterMax = 1.5
)

// API path templates. Static endpoints are exported as constants; dynamic
// endpoints (those containing a document ID) are exposed as functions that
// URL-encode the ID via net/url's PathEscape.
const (
	PathRender        = "/v1/render"
	PathRenderPreview = "/v1/render/preview"
)

// PathDocument returns the GET / DELETE path for a single stored document.
func PathDocument(id string) string {
	return "/v1/documents/" + url.PathEscape(id)
}

// PathDocumentPreview returns the GET path for a stored document's paginated
// HTML preview.
func PathDocumentPreview(id string) string {
	return "/v1/documents/" + url.PathEscape(id) + "/preview"
}

// PathDocumentThumbnails returns the POST path for the thumbnail-generation
// endpoint of a stored document.
func PathDocumentThumbnails(id string) string {
	return "/v1/documents/" + url.PathEscape(id) + "/thumbnails"
}

// Canonical HTTP header names. Centralised so individual call sites cannot
// drift on capitalisation; net/http canonicalises on Set but Get-by-string
// is case-sensitive on raw maps.
const (
	HeaderAuthorization     = "Authorization"
	HeaderContentType       = "Content-Type"
	HeaderAccept            = "Accept"
	HeaderUserAgent         = "User-Agent"
	HeaderIdempotencyKey    = "Idempotency-Key"
	HeaderRetryAfter        = "Retry-After"
	HeaderRequestID         = "X-Request-Id"
	HeaderDocumentPageCount = "X-Document-Page-Count"
)
