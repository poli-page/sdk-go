// Package http contains the pure transport-layer helpers used by the
// top-level polipage client. None of these functions perform I/O — they
// build values that the orchestrator hands to net/http, or parse values
// that the orchestrator already received.
//
// Mirrors src/internal/http.ts in the Node SDK.
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

// ComputeBackoff returns the exponential-backoff delay before retry
// attempt (1-based: 1 means the first retry), used when the failed answer
// carried no usable Retry-After header:
// baseDelay × 2^(attempt-1) × jitterFactor, jitterFactor uniform in
// [0.5, 1.5).
//
// The jitter source is math/rand/v2's package-level Float64, which is
// auto-seeded with crypto-grade entropy and cannot be re-seeded by other
// code in the process — the property the Node SDK approximates via a
// package-scoped *rand.Rand.
func ComputeBackoff(attempt int, baseDelay time.Duration) time.Duration {
	exp := float64(baseDelay) * math.Pow(2, float64(attempt-1))
	jitterFactor := 0.5 + rand.Float64() //nolint:gosec // G404: jitter is not security-sensitive; math/rand/v2 is correct here
	return time.Duration(exp * jitterFactor)
}

// ParseErrorBody parses a non-2xx response body into a (code, message) pair.
//
// Code is the body's code field, else its error field, else
// "unknown_error"; it is copied verbatim and never inferred from the status
// or the message. Message is detail, else title, else message, else
// "HTTP <status>". Fields that are not strings are ignored. A body that is
// not JSON (a gateway's HTML page, an empty body) gives "INTERNAL_ERROR"
// and "HTTP <status>"; the caller keeps the real status.
func ParseErrorBody(body []byte, status int) (code, message string) {
	fallback := fmt.Sprintf("HTTP %d", status)
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "INTERNAL_ERROR", fallback
	}
	fields, _ := parsed.(map[string]any)
	str := func(name string) string {
		s, _ := fields[name].(string)
		return s
	}
	code = firstNonEmpty(str("code"), str("error"), "unknown_error")
	message = firstNonEmpty(str("detail"), str("title"), str("message"), fallback)
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

// ParseRetryAfter interprets a Retry-After header value: an integer
// number of seconds or an HTTP-date. It returns the delay clamped to
// [0, RetryAfterCap] (a date in the past gives 0, an integer too large for
// any integer type gives the cap), and false when the value is empty or in
// any other form ("1.5", "-1", "soon"), so the caller falls back to the
// computed backoff.
func ParseRetryAfter(headerValue string) (time.Duration, bool) {
	if headerValue == "" {
		return 0, false
	}
	if isDigits(headerValue) {
		secs, err := strconv.ParseInt(headerValue, 10, 64)
		if err != nil || secs > int64(constants.RetryAfterCap/time.Second) {
			// Only overflow can fail on digits: far above the cap either way.
			return constants.RetryAfterCap, true
		}
		return time.Duration(secs) * time.Second, true
	}
	if when, err := http.ParseTime(headerValue); err == nil {
		return clampDuration(time.Until(when)), true
	}
	return 0, false
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
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
