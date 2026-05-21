package http

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

const userAgent = "poli-page-sdk-go/0.0.0-dev"

func TestBuildURL_joinsBaseAndPath(t *testing.T) {
	t.Parallel()
	got := BuildURL("https://api.poli.page", "/v1/render")
	want := "https://api.poli.page/v1/render"
	if got != want {
		t.Fatalf("BuildURL = %q, want %q", got, want)
	}
}

func TestBuildURL_stripsTrailingSlashFromBase(t *testing.T) {
	t.Parallel()
	got := BuildURL("https://api.poli.page/", "/v1/render")
	want := "https://api.poli.page/v1/render"
	if got != want {
		t.Fatalf("BuildURL = %q, want %q", got, want)
	}
}

func TestBuildURL_addsLeadingSlashWhenMissing(t *testing.T) {
	t.Parallel()
	got := BuildURL("https://api.poli.page", "v1/render")
	want := "https://api.poli.page/v1/render"
	if got != want {
		t.Fatalf("BuildURL = %q, want %q", got, want)
	}
}

func TestBuildHeaders_alwaysSetsAcceptJSON(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		h := BuildHeaders(method, "pp_test_x", "idem-1", userAgent)
		if got := h.Get("Accept"); got != "application/json" {
			t.Errorf("method=%s Accept=%q, want application/json", method, got)
		}
	}
}

func TestBuildHeaders_postSetsContentTypeJSON(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodPost, "pp_test_x", "idem-1", userAgent)
	if got := h.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func TestBuildHeaders_setsAuthorizationBearer(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodPost, "pp_test_xyz", "idem-1", userAgent)
	if got := h.Get("Authorization"); got != "Bearer pp_test_xyz" {
		t.Fatalf("Authorization = %q, want Bearer pp_test_xyz", got)
	}
}

func TestBuildHeaders_setsUserAgentVerbatim(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodPost, "pp_test_x", "idem-1", "custom-ua/9.9.9")
	if got := h.Get("User-Agent"); got != "custom-ua/9.9.9" {
		t.Fatalf("User-Agent = %q, want custom-ua/9.9.9", got)
	}
}

func TestBuildHeaders_postSetsIdempotencyKey(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodPost, "pp_test_x", "idem-abc-123", userAgent)
	if got := h.Get("Idempotency-Key"); got != "idem-abc-123" {
		t.Fatalf("Idempotency-Key = %q, want idem-abc-123", got)
	}
}

func TestBuildHeaders_postSkipsIdempotencyKeyWhenEmpty(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodPost, "pp_test_x", "", userAgent)
	if values, ok := h["Idempotency-Key"]; ok {
		t.Fatalf("Idempotency-Key should be absent when empty, got %v", values)
	}
}

func TestBuildHeaders_getOmitsContentTypeAndIdempotencyKey(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodGet, "pp_test_x", "idem-ignored", userAgent)
	if _, ok := h["Content-Type"]; ok {
		t.Errorf("GET should not set Content-Type, got %q", h.Get("Content-Type"))
	}
	if _, ok := h["Idempotency-Key"]; ok {
		t.Errorf("GET should not set Idempotency-Key, got %q", h.Get("Idempotency-Key"))
	}
	if got := h.Get("Authorization"); got != "Bearer pp_test_x" {
		t.Errorf("GET Authorization = %q, want Bearer pp_test_x", got)
	}
	if got := h.Get("Accept"); got != "application/json" {
		t.Errorf("GET Accept = %q, want application/json", got)
	}
	if got := h.Get("User-Agent"); got != userAgent {
		t.Errorf("GET User-Agent = %q, want %q", got, userAgent)
	}
}

func TestBuildHeaders_deleteOmitsContentTypeAndIdempotencyKey(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodDelete, "pp_test_x", "idem-ignored", userAgent)
	if _, ok := h["Content-Type"]; ok {
		t.Errorf("DELETE should not set Content-Type")
	}
	if _, ok := h["Idempotency-Key"]; ok {
		t.Errorf("DELETE should not set Idempotency-Key")
	}
}

func TestParseRetryAfter_emptyStringReturnsFalse(t *testing.T) {
	t.Parallel()
	d, ok := ParseRetryAfter("")
	if ok || d != 0 {
		t.Fatalf("ParseRetryAfter(\"\") = (%v, %v), want (0, false)", d, ok)
	}
}

func TestParseRetryAfter_zeroSeconds(t *testing.T) {
	t.Parallel()
	d, ok := ParseRetryAfter("0")
	if !ok || d != 0 {
		t.Fatalf("ParseRetryAfter(\"0\") = (%v, %v), want (0, true)", d, ok)
	}
}

func TestParseRetryAfter_integerSeconds(t *testing.T) {
	t.Parallel()
	d, ok := ParseRetryAfter("5")
	if !ok || d != 5*time.Second {
		t.Fatalf("ParseRetryAfter(\"5\") = (%v, %v), want (5s, true)", d, ok)
	}
}

func TestParseRetryAfter_capsLargeSeconds(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"999", "100000"} {
		d, ok := ParseRetryAfter(in)
		if !ok || d != 30*time.Second {
			t.Errorf("ParseRetryAfter(%q) = (%v, %v), want (30s, true)", in, d, ok)
		}
	}
}

func TestParseRetryAfter_unparseableReturnsFalse(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"abc", "not a date"} {
		d, ok := ParseRetryAfter(in)
		if ok || d != 0 {
			t.Errorf("ParseRetryAfter(%q) = (%v, %v), want (0, false)", in, d, ok)
		}
	}
}

func TestParseRetryAfter_pastHTTPDateClampsToZero(t *testing.T) {
	t.Parallel()
	past := time.Now().UTC().Add(-60 * time.Second).Format(http.TimeFormat)
	d, ok := ParseRetryAfter(past)
	if !ok {
		t.Fatalf("ParseRetryAfter(past) ok = false, want true (HTTP-date parsed, just clamped)")
	}
	if d != 0 {
		t.Fatalf("ParseRetryAfter(past) = %v, want 0 (clamped)", d)
	}
}

func TestParseRetryAfter_futureHTTPDateReturnsDelta(t *testing.T) {
	t.Parallel()
	future := time.Now().UTC().Add(5 * time.Second).Format(http.TimeFormat)
	d, ok := ParseRetryAfter(future)
	if !ok {
		t.Fatalf("ParseRetryAfter(future) ok = false")
	}
	if d < 3*time.Second || d > 5*time.Second {
		t.Fatalf("ParseRetryAfter(future) = %v, want in [3s, 5s]", d)
	}
}

func TestParseRetryAfter_farFutureCapsAt30s(t *testing.T) {
	t.Parallel()
	farFuture := time.Now().UTC().Add(60 * time.Minute).Format(http.TimeFormat)
	d, ok := ParseRetryAfter(farFuture)
	if !ok || d != 30*time.Second {
		t.Fatalf("ParseRetryAfter(farFuture) = (%v, %v), want (30s, true)", d, ok)
	}
}

func TestParseErrorBody_extractsCodeAndMessage(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte(`{"code":"VALIDATION_ERROR","message":"data is required"}`), 400)
	if code != "VALIDATION_ERROR" || msg != "data is required" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (VALIDATION_ERROR, data is required)", code, msg)
	}
}

func TestParseErrorBody_fallsBackToMessageAsCode(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte(`{"message":"something broke"}`), 400)
	if code != "something broke" || msg != "something broke" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (something broke, something broke)", code, msg)
	}
}

func TestParseErrorBody_fallsBackToErrorAsCode(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte(`{"error":"oops"}`), 400)
	if code != "oops" || msg != "API error (400): oops" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (oops, API error (400): oops)", code, msg)
	}
}

func TestParseErrorBody_unknownErrorWhenNoRecognisedFields(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte(`{}`), 400)
	if code != "unknown_error" || msg != "API error (400): unknown_error" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (unknown_error, API error (400): unknown_error)", code, msg)
	}
}

func TestParseErrorBody_internalErrorOnInvalidJSON(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte("not json"), 502)
	if code != "INTERNAL_ERROR" || msg != "API error 502: response body was not valid JSON" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (INTERNAL_ERROR, API error 502: response body was not valid JSON)", code, msg)
	}
}

func TestParseErrorBody_internalErrorOnHTML(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte("<html>upstream gone</html>"), 502)
	if code != "INTERNAL_ERROR" {
		t.Errorf("ParseErrorBody code = %q, want INTERNAL_ERROR", code)
	}
	if !strings.Contains(msg, "502") {
		t.Errorf("ParseErrorBody msg = %q, want to contain 502", msg)
	}
}

func TestParseErrorBody_internalErrorOnEmptyBody(t *testing.T) {
	t.Parallel()
	code, _ := ParseErrorBody([]byte(""), 500)
	if code != "INTERNAL_ERROR" {
		t.Fatalf("ParseErrorBody empty body code = %q, want INTERNAL_ERROR", code)
	}
}

func TestComputeBackoff_returnsRetryAfterAsIsWhenPositive(t *testing.T) {
	t.Parallel()
	// When the server tells us how long to wait, honor it verbatim — no jitter
	// even across many calls.
	for i := 0; i < 20; i++ {
		if got := ComputeBackoff(1, 500*time.Millisecond, 1*time.Second); got != 1*time.Second {
			t.Fatalf("ComputeBackoff(retryAfter=1s) iter %d = %v, want 1s exactly", i, got)
		}
		if got := ComputeBackoff(3, 500*time.Millisecond, 250*time.Millisecond); got != 250*time.Millisecond {
			t.Fatalf("ComputeBackoff(retryAfter=250ms) iter %d = %v, want 250ms exactly", i, got)
		}
	}
}

func TestComputeBackoff_zeroRetryAfterFallsThroughToBackoff(t *testing.T) {
	t.Parallel()
	// retryAfter=0 means "no server override" in Go (the time.Duration zero
	// value); the helper falls through to computed exponential backoff with
	// jitter. Documented divergence from Node, see sdk-go-plan.md §6.
	for i := 0; i < 50; i++ {
		got := ComputeBackoff(1, 1*time.Second, 0)
		if got < 500*time.Millisecond || got >= 1500*time.Millisecond {
			t.Fatalf("ComputeBackoff(1, 1s, 0) iter %d = %v, want in [500ms, 1500ms)", i, got)
		}
	}
}

func TestComputeBackoff_exponentialGrowthByAttempt(t *testing.T) {
	t.Parallel()
	// attempt=n → base * 2^(n-1) * jitter where jitter is in [0.5, 1.5).
	// Sample many times per attempt; the (min, max) seen must lie inside the
	// expected band for that attempt and outside the adjacent bands' midpoints.
	const samples = 500
	base := 500 * time.Millisecond
	for attempt, exp := range []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second} {
		n := attempt + 1
		lo := time.Duration(float64(exp) * 0.5)
		hi := time.Duration(float64(exp) * 1.5)
		for i := 0; i < samples; i++ {
			got := ComputeBackoff(n, base, 0)
			if got < lo || got >= hi {
				t.Fatalf("ComputeBackoff(attempt=%d, base=%v, 0) iter %d = %v, want in [%v, %v)", n, base, i, got, lo, hi)
			}
		}
	}
}

func TestComputeBackoff_jitterCoversFullBand(t *testing.T) {
	t.Parallel()
	// Over many samples we should see values near both ends of [0.5*base, 1.5*base).
	// This catches a regression where jitter collapses to a constant.
	const samples = 1000
	base := 1 * time.Second
	var minSeen, maxSeen time.Duration = 1 << 62, 0
	for i := 0; i < samples; i++ {
		d := ComputeBackoff(1, base, 0)
		if d < minSeen {
			minSeen = d
		}
		if d > maxSeen {
			maxSeen = d
		}
	}
	// The band is [500ms, 1500ms). Demand at least 80% coverage either side.
	if minSeen > 600*time.Millisecond {
		t.Errorf("min jitter sample = %v, expected something below 600ms over %d samples", minSeen, samples)
	}
	if maxSeen < 1400*time.Millisecond {
		t.Errorf("max jitter sample = %v, expected something above 1400ms over %d samples", maxSeen, samples)
	}
}
