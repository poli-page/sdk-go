// Package http contains the pure transport-layer helpers used by the
// top-level polipage client. None of these functions perform I/O — they
// build values that the orchestrator hands to net/http, or parse values
// that the orchestrator already received.
//
// Mirrors src/internal/http.ts in the Node SDK (see sdk-go-plan.md §3.1).
package http

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/poli-page/sdk-go/internal/constants"
)

// BuildURL joins the API base URL and an endpoint path, tolerating extra
// or missing slashes on either side. The returned string is the absolute
// URL net/http will request.
func BuildURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// ComputeBackoff returns the delay before the next retry attempt.
//
// A positive retryAfter is returned verbatim (the server told us how long
// to wait, no jitter). When retryAfter is zero — the time.Duration zero
// value, treated as "no server override" — the result is
// baseDelay × 2^(attempt-1) × jitterFactor where jitterFactor is uniform
// in [0.5, 1.5). attempt is 1-based: 1 means the first retry.
//
// The jitter source is math/rand/v2's package-level Float64, which is
// auto-seeded with crypto-grade entropy and cannot be re-seeded by other
// code in the process — the property the Node SDK approximates via a
// package-scoped *rand.Rand.
func ComputeBackoff(attempt int, baseDelay, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	exp := float64(baseDelay) * math.Pow(2, float64(attempt-1))
	jitterFactor := 0.5 + rand.Float64() //nolint:gosec // G404: jitter is not security-sensitive; math/rand/v2 is correct here
	return time.Duration(exp * jitterFactor)
}

// ParseErrorBody parses a non-2xx response body into a (code, message) pair.
//
// The API speaks RFC 7807 ProblemDetails. Message preference: detail
// (specific reason) → title (generic problem name) → message (legacy
// non-7807 endpoints) → short canned "HTTP <status>". Code is verbatim
// from the API (fallback "unknown_error"); we never invent a code from
// the message. Bodies that aren't valid JSON objects surface as
// "INTERNAL_ERROR".
func ParseErrorBody(body []byte, status int) (code, message string) {
	var parsed struct {
		Code    string `json:"code"`
		Detail  string `json:"detail"`
		Title   string `json:"title"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "INTERNAL_ERROR", fmt.Sprintf("HTTP %d: response body was not valid JSON", status)
	}
	code = firstNonEmpty(parsed.Code, parsed.Error, "unknown_error")
	message = firstNonEmpty(parsed.Detail, parsed.Title, parsed.Message, fmt.Sprintf("HTTP %d", status))
	return code, message
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ParseRetryAfter interprets a Retry-After header value. It accepts an
// integer number of seconds or an RFC 7231 HTTP-date, returns the delay
// clamped to [0, RetryAfterCap], and signals false when the value is empty
// or unparseable so the caller can fall back to computed backoff.
func ParseRetryAfter(headerValue string) (time.Duration, bool) {
	if headerValue == "" {
		return 0, false
	}
	if secs, err := strconv.ParseFloat(headerValue, 64); err == nil {
		return clampDuration(time.Duration(secs * float64(time.Second))), true
	}
	if when, err := http.ParseTime(headerValue); err == nil {
		return clampDuration(time.Until(when)), true
	}
	return 0, false
}

func clampDuration(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	if d > constants.RetryAfterCap {
		return constants.RetryAfterCap
	}
	return d
}

// BuildHeaders returns the standard request header set used for every
// SDK-originated request. POST requests get Content-Type and (when
// non-empty) Idempotency-Key; GET and DELETE omit both. Accept is always
// application/json — every deployed SDK endpoint returns JSON; PDF bytes
// come from a separate plain fetch against the presigned S3 URL.
func BuildHeaders(method, apiKey, idempotencyKey, userAgent string) http.Header {
	h := make(http.Header, 5)
	h.Set("Accept", "application/json")
	h.Set("Authorization", "Bearer "+apiKey)
	h.Set("User-Agent", userAgent)
	if method == http.MethodPost {
		h.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			h.Set("Idempotency-Key", idempotencyKey)
		}
	}
	return h
}
