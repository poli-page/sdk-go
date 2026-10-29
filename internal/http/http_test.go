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
		h := BuildHeaders(method, "pp_sandbox_x", "idem-1", userAgent)
		if got := h.Get("Accept"); got != "application/json" {
			t.Errorf("method=%s Accept=%q, want application/json", method, got)
		}
	}
}

func TestBuildHeaders_postSetsContentTypeJSON(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodPost, "pp_sandbox_x", "idem-1", userAgent)
	if got := h.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func TestBuildHeaders_setsAuthorizationBearer(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodPost, "pp_sandbox_xyz", "idem-1", userAgent)
	if got := h.Get("Authorization"); got != "Bearer pp_sandbox_xyz" {
		t.Fatalf("Authorization = %q, want Bearer pp_sandbox_xyz", got)
	}
}

func TestBuildHeaders_setsUserAgentVerbatim(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodPost, "pp_sandbox_x", "idem-1", "custom-ua/9.9.9")
	if got := h.Get("User-Agent"); got != "custom-ua/9.9.9" {
		t.Fatalf("User-Agent = %q, want custom-ua/9.9.9", got)
	}
}

func TestBuildHeaders_postSetsIdempotencyKey(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodPost, "pp_sandbox_x", "idem-abc-123", userAgent)
	if got := h.Get("Idempotency-Key"); got != "idem-abc-123" {
		t.Fatalf("Idempotency-Key = %q, want idem-abc-123", got)
	}
}

func TestBuildHeaders_postSkipsIdempotencyKeyWhenEmpty(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodPost, "pp_sandbox_x", "", userAgent)
	if values, ok := h["Idempotency-Key"]; ok {
		t.Fatalf("Idempotency-Key should be absent when empty, got %v", values)
	}
}

func TestBuildHeaders_getOmitsContentTypeAndIdempotencyKey(t *testing.T) {
	t.Parallel()
	h := BuildHeaders(http.MethodGet, "pp_sandbox_x", "idem-ignored", userAgent)
	if _, ok := h["Content-Type"]; ok {
		t.Errorf("GET should not set Content-Type, got %q", h.Get("Content-Type"))
	}
	if _, ok := h["Idempotency-Key"]; ok {
		t.Errorf("GET should not set Idempotency-Key, got %q", h.Get("Idempotency-Key"))
	}
	if got := h.Get("Authorization"); got != "Bearer pp_sandbox_x" {
		t.Errorf("GET Authorization = %q, want Bearer pp_sandbox_x", got)
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
	h := BuildHeaders(http.MethodDelete, "pp_sandbox_x", "idem-ignored", userAgent)
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

func TestParseRetryAfter_capsIntegersBeyondInt64(t *testing.T) {
	t.Parallel()
	// Overflows int64 (and time.Duration): clamps to the cap, never wraps to a
	// negative or zero delay.
	for _, in := range []string{"9223372036854775808", "99999999999999999999999999999999"} {
		d, ok := ParseRetryAfter(in)
		if !ok || d != 30*time.Second {
			t.Errorf("ParseRetryAfter(%q) = (%v, %v), want (30s, true)", in, d, ok)
		}
	}
}

func TestParseRetryAfter_unparseableReturnsFalse(t *testing.T) {
	t.Parallel()
	// Only integer seconds and HTTP-dates are read; anything else falls back
	// to the computed backoff.
	for _, in := range []string{"abc", "not a date", "1.5", "-1", "+5", "1e3", "NaN", "Inf", "5s", " "} {
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

func TestParseErrorBody_codeStaysUnknownWhenOnlyMessagePresent(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte(`{"message":"something broke"}`), 400)
	if code != "unknown_error" || msg != "something broke" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (unknown_error, something broke)", code, msg)
	}
}

func TestParseErrorBody_fallsBackToErrorAsCode(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte(`{"error":"oops"}`), 400)
	if code != "oops" || msg != "HTTP 400" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (oops, HTTP 400)", code, msg)
	}
}

func TestParseErrorBody_unknownErrorWhenNoRecognisedFields(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte(`{}`), 400)
	if code != "unknown_error" || msg != "HTTP 400" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (unknown_error, HTTP 400)", code, msg)
	}
}

func TestParseErrorBody_internalErrorOnInvalidJSON(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte("not json"), 502)
	if code != "INTERNAL_ERROR" || msg != "HTTP 502" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (INTERNAL_ERROR, HTTP 502)", code, msg)
	}
}

func TestParseErrorBody_ignoresFieldsThatAreNotStrings(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte(`{"code":42,"error":"QUOTA_EXCEEDED","detail":{"x":1},"title":"Quota"}`), 429)
	if code != "QUOTA_EXCEEDED" || msg != "Quota" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (QUOTA_EXCEEDED, Quota)", code, msg)
	}
}

func TestParseErrorBody_jsonThatIsNotAnObject(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`[]`, `"oops"`, `null`, `7`} {
		code, msg := ParseErrorBody([]byte(body), 500)
		if code != "unknown_error" || msg != "HTTP 500" {
			t.Errorf("ParseErrorBody(%s) = (%q, %q), want (unknown_error, HTTP 500)", body, code, msg)
		}
	}
}

func TestParseErrorBody_usesRFC7807Detail(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody(
		[]byte(`{"code":"authentication_failed","detail":"Forbidden","title":"Authentication failed"}`),
		401,
	)
	if code != "authentication_failed" || msg != "Forbidden" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (authentication_failed, Forbidden)", code, msg)
	}
}

func TestParseErrorBody_fallsBackToTitleWhenDetailAbsent(t *testing.T) {
	t.Parallel()
	code, msg := ParseErrorBody([]byte(`{"code":"forbidden","title":"Access denied"}`), 403)
	if code != "forbidden" || msg != "Access denied" {
		t.Fatalf("ParseErrorBody = (%q, %q), want (forbidden, Access denied)", code, msg)
	}
}

func TestParseErrorBody_noSynthesisedAPIErrorPrefix(t *testing.T) {
	t.Parallel()
	_, msg := ParseErrorBody([]byte(`{"code":"THUMBNAILS_NOT_AVAILABLE"}`), 403)
	if strings.Contains(msg, "API error") {
		t.Fatalf("ParseErrorBody msg = %q, must not contain 'API error' synthesis", msg)
	}
	if msg != "HTTP 403" {
		t.Fatalf("ParseErrorBody msg = %q, want HTTP 403", msg)
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
			got := ComputeBackoff(n, base)
			if got < lo || got >= hi {
				t.Fatalf("ComputeBackoff(attempt=%d, base=%v) iter %d = %v, want in [%v, %v)", n, base, i, got, lo, hi)
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
		d := ComputeBackoff(1, base)
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
