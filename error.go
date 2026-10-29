package polipage

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Error is the single error type raised by the SDK: API errors, network
// failures, timeouts, caller cancellation, and constructor validation
// failures all surface as *Error.
//
// Callers can use either errors.Is against the package-level sentinels
// (ErrNotFound, ErrUnauthorized, etc.) for quick branching or errors.As
// to inspect Code, StatusCode, RequestID, and the wrapped Cause.
type Error struct {
	// Code is the machine-readable error code. For HTTP errors this is the
	// API's code field (verbatim); for SDK-internal errors it is one of
	// ErrCode* (invalid_options, network_error, timeout, aborted, etc.).
	Code string
	// StatusCode is the HTTP response status. Zero for SDK-internal errors
	// (network failure, timeout, abort, constructor validation).
	StatusCode int
	// Message is the human-readable error message.
	Message string
	// RequestID is the x-request-id header value when present; empty otherwise.
	RequestID string
	// Cause is the underlying error this *Error wraps. nil when the error
	// originated in the SDK itself with no upstream cause.
	Cause error
}

// Error implements the error interface. The format is stable but not parseable
// — callers wanting structured access should use errors.As to extract *Error.
func (e *Error) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("polipage: %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("polipage: %s [%d]: %s", e.Code, e.StatusCode, e.Message)
}

// Unwrap exposes the wrapped Cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// Payload is the canonical wire shape for framework integrations:
// {code, message, status, requestId}. Status is the API HTTP status for
// status-bearing failures, 503 for network failures, 504 for timeouts, and
// 0 otherwise. In JSON, a zero Status and an empty RequestID are null.
//
// Do not embed Payload in another struct: its MarshalJSON is promoted, so
// the embedding struct marshals as the Payload alone and its own fields
// drop out of the JSON. Use a named field instead.
type Payload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Status    int    `json:"status"`
	RequestID string `json:"requestId"`
}

// MarshalJSON writes Status and RequestID as null when they are unknown
// (zero and empty), as the SDK specification's toPayload() does.
func (p Payload) MarshalJSON() ([]byte, error) {
	var status *int
	if p.Status != 0 {
		status = &p.Status
	}
	var requestID *string
	if p.RequestID != "" {
		requestID = &p.RequestID
	}
	return json.Marshal(struct {
		Code      string  `json:"code"`
		Message   string  `json:"message"`
		Status    *int    `json:"status"`
		RequestID *string `json:"requestId"`
	}{p.Code, p.Message, status, requestID})
}

// ToPayload returns the canonical wire payload for framework integrations.
// The Error.StatusCode field stays 0 for transport failures — only the
// payload surfaces 503/504, so callers reading StatusCode are not affected.
func (e *Error) ToPayload() Payload {
	status := e.StatusCode
	if status == 0 {
		switch e.Code {
		case ErrCodeTimeout:
			status = 504
		case ErrCodeNetworkError:
			status = 503
		}
	}
	return Payload{
		Code:      e.Code,
		Message:   e.Message,
		Status:    status,
		RequestID: e.RequestID,
	}
}

// Is supports errors.Is by matching the receiver against package-level
// sentinel errors via their Code field. A small number of logical groups
// (auth: MISSING_API_KEY / INVALID_API_KEY; rate-limit: QUOTA_EXCEEDED /
// OVERAGE_CAP_EXCEEDED) compare equal so the corresponding sentinel
// matches either underlying code.
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	if e.Code == t.Code {
		return true
	}
	return inAuthGroup(e.Code) && inAuthGroup(t.Code) ||
		inRateLimitGroup(e.Code) && inRateLimitGroup(t.Code)
}

func inAuthGroup(code string) bool {
	return code == ErrCodeMissingAPIKey || code == ErrCodeInvalidAPIKey
}

func inRateLimitGroup(code string) bool {
	return code == ErrCodeQuotaExceeded || code == ErrCodeOverageCapExceeded
}

// IsAuthError reports whether the response was 401 or 403 — invalid,
// missing, or unauthorized API key.
func (e *Error) IsAuthError() bool { return e.StatusCode == 401 || e.StatusCode == 403 }

// IsRateLimitError reports whether the response was 429 — too many requests.
func (e *Error) IsRateLimitError() bool { return e.StatusCode == 429 }

// IsValidationError reports whether the response was 400 — request payload
// failed validation.
func (e *Error) IsValidationError() bool { return e.StatusCode == 400 }

// IsNetworkError reports whether the error is a transport-level failure
// (DNS, connection refused, TLS) or a per-request timeout. Caller-aborted
// requests (ErrCodeAborted) and constructor-validation failures
// (ErrCodeInvalidOptions) do not count.
func (e *Error) IsNetworkError() bool {
	return e.Code == ErrCodeNetworkError || e.Code == ErrCodeTimeout
}

// IsRetryable reports whether the SDK considers this error retryable: a
// network failure, a timeout, a 5xx or a 429, except QUOTA_EXCEEDED,
// OVERAGE_CAP_EXCEEDED and STORAGE_ACCESS_DENIED, which last until the
// monthly reset or until the bucket access is restored. Caller-aborted
// requests are never retryable. The SDK already retries internally up to
// the configured limit; this predicate is useful when an outer queue or
// scheduler decides whether to re-enqueue the job.
func (e *Error) IsRetryable() bool {
	if e.Code == ErrCodeAborted || isNonRetryableCode(e.Code) {
		return false
	}
	if e.IsNetworkError() {
		return true
	}
	if e.StatusCode >= 500 {
		return true
	}
	return e.StatusCode == 429
}

// isNonRetryableCode reports the codes that are never retried although
// their status is (a 429 or a 5xx): waiting and trying again cannot help.
func isNonRetryableCode(code string) bool {
	switch code {
	case ErrCodeQuotaExceeded, ErrCodeOverageCapExceeded, ErrCodeStorageAccessDenied:
		return true
	}
	return false
}

// SDK-internal error codes. These do not appear in API responses; the SDK
// sets them on errors raised before, around, or after the HTTP call.
const (
	// ErrCodeInvalidOptions marks constructor or per-call option validation
	// failures (empty API key, malformed metadata, etc.).
	ErrCodeInvalidOptions = "invalid_options"
	// ErrCodeNetworkError marks DNS / connection / TLS failures.
	ErrCodeNetworkError = "network_error"
	// ErrCodeTimeout marks a per-request deadline being exceeded.
	ErrCodeTimeout = "timeout"
	// ErrCodeAborted marks caller-initiated cancellation (a canceled
	// context.Context). Never retryable.
	ErrCodeAborted = "aborted"
	// ErrCodeUnknownError is the fallback code when an error-body JSON has
	// no recognised fields.
	ErrCodeUnknownError = "unknown_error"
	// ErrCodeDownloadFailed marks a failure fetching a presigned PDF URL.
	ErrCodeDownloadFailed = "DOWNLOAD_FAILED"
	// ErrCodeInternalError is the SDK-side fallback when the response body
	// is unparseable or missing. Distinct from ErrCodeAPIInternalError, which
	// has the same wire value but originates from the API.
	ErrCodeInternalError = "INTERNAL_ERROR"
	// ErrCodeIOFailed marks local filesystem failures from [RenderToFile]
	// (MkdirAll / Create / write errors). The underlying os error is wrapped
	// in Cause — use errors.Unwrap or errors.As(*os.PathError) to inspect.
	ErrCodeIOFailed = "io_failed"
)

// Known API error codes pass through verbatim from the deployed API.
// Callers may see codes not in this list — the SDK preserves whatever the
// API returns.
const (
	ErrCodeMissingAPIKey              = "MISSING_API_KEY" //nolint:gosec // G101: error-code identifier, not a credential
	ErrCodeInvalidAPIKey              = "INVALID_API_KEY" //nolint:gosec // G101: error-code identifier, not a credential
	ErrCodeForbidden                  = "FORBIDDEN"
	ErrCodeOrganizationCancelled      = "ORGANIZATION_CANCELLED"
	ErrCodeOrganizationPurged         = "ORGANIZATION_PURGED"
	ErrCodeOrganizationPaymentGrace   = "ORGANIZATION_PAYMENT_GRACE"
	ErrCodeOrganizationMigrating      = "ORGANIZATION_MIGRATING"
	ErrCodeEnvironmentMismatch        = "ENVIRONMENT_MISMATCH"
	ErrCodeEnvironmentProtected       = "ENVIRONMENT_PROTECTED"
	ErrCodeNotFound                   = "NOT_FOUND"
	ErrCodeProjectNotFound            = "PROJECT_NOT_FOUND"
	ErrCodeProjectNotInEnvironment    = "PROJECT_NOT_IN_ENVIRONMENT"
	ErrCodeTemplateNotFound           = "TEMPLATE_NOT_FOUND"
	ErrCodeDocumentNotFound           = "DOCUMENT_NOT_FOUND"
	ErrCodeDocumentGone               = "DOCUMENT_GONE"
	ErrCodeValidationError            = "VALIDATION_ERROR"
	ErrCodeMissingData                = "MISSING_DATA"
	ErrCodeMissingTemplateSlug        = "MISSING_TEMPLATE_SLUG"
	ErrCodeProjectRequiredForDocument = "PROJECT_REQUIRED_FOR_DOCUMENT"
	ErrCodeTemplateError              = "TEMPLATE_ERROR"
	ErrCodeTemplateUnsafe             = "TEMPLATE_UNSAFE"
	ErrCodeEmptyDocument              = "EMPTY_DOCUMENT"
	ErrCodeRenderBodyTooLarge         = "RENDER_BODY_TOO_LARGE"
	ErrCodeDocumentTooLarge           = "DOCUMENT_TOO_LARGE"
	ErrCodeThumbnailsNotAvailable     = "THUMBNAILS_NOT_AVAILABLE"
	ErrCodeQuotaExceeded              = "QUOTA_EXCEEDED"
	ErrCodeOverageCapExceeded         = "OVERAGE_CAP_EXCEEDED"
	ErrCodeRateLimitExceeded          = "RATE_LIMIT_EXCEEDED"
	// ErrCodePublishInProgress is a 503 answered while a publish locks the
	// project in the key's environment. Retried like any 5xx, honouring
	// Retry-After.
	ErrCodePublishInProgress   = "PUBLISH_IN_PROGRESS"
	ErrCodeStorageAccessDenied = "STORAGE_ACCESS_DENIED"
	ErrCodeRenderTimeout       = "RENDER_TIMEOUT"
	// ErrCodeAPIInternalError shares the wire value "INTERNAL_ERROR" with
	// ErrCodeInternalError; kept as a separate constant for call-site clarity.
	ErrCodeAPIInternalError = "INTERNAL_ERROR"
)

// Sentinel errors. Use with errors.Is for ergonomic call-site checks:
//
//	if errors.Is(err, polipage.ErrNotFound) {
//	    return nil // treat 404 as absent without further inspection
//	}
//
// Each sentinel is a zero-status *Error keyed by Code; (*Error).Is matches
// receivers whose Code equals the sentinel's, with two logical groups
// (auth, rate-limit) that compare equal across related codes.
var (
	ErrUnauthorized          = &Error{Code: ErrCodeMissingAPIKey}         // also matches INVALID_API_KEY
	ErrForbidden             = &Error{Code: ErrCodeForbidden}             //
	ErrNotFound              = &Error{Code: ErrCodeNotFound}              //
	ErrDocumentNotFound      = &Error{Code: ErrCodeDocumentNotFound}      //
	ErrDocumentGone          = &Error{Code: ErrCodeDocumentGone}          // 410 — document deleted or expired
	ErrValidation            = &Error{Code: ErrCodeValidationError}       //
	ErrRateLimit             = &Error{Code: ErrCodeQuotaExceeded}         // also matches OVERAGE_CAP_EXCEEDED
	ErrTimeout               = &Error{Code: ErrCodeTimeout}               //
	ErrAborted               = &Error{Code: ErrCodeAborted}               //
	ErrNetwork               = &Error{Code: ErrCodeNetworkError}          //
	ErrDownloadFailed        = &Error{Code: ErrCodeDownloadFailed}        //
	ErrIOFailed              = &Error{Code: ErrCodeIOFailed}              // local filesystem write failure from RenderToFile
	ErrOrganizationCancelled = &Error{Code: ErrCodeOrganizationCancelled} // 403 — subscription cancelled, service is read-only
	ErrOrganizationPurged    = &Error{Code: ErrCodeOrganizationPurged}    // 410 — organization data has been purged
)
