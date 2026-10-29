package polipage_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// failingServer answers every request with status, the headers and an API error body of code.
func failingServer(t *testing.T, status int, code string, headers map[string]string, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":"`+code+`","detail":"from the test"}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRetry_neverRetriedCodes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status int
		code   string
	}{
		{429, polipage.ErrCodeQuotaExceeded},
		{429, polipage.ErrCodeOverageCapExceeded},
		{502, polipage.ErrCodeStorageAccessDenied},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := failingServer(t, tc.status, tc.code, map[string]string{"Retry-After": "3600"}, &calls)
			client := newTestClient(t, server)
			_, err := client.Render.Document(context.Background(), minimalInput())
			var pErr *polipage.Error
			if !errors.As(err, &pErr) || pErr.Code != tc.code || pErr.StatusCode != tc.status {
				t.Fatalf("err = %v, want %s [%d]", err, tc.code, tc.status)
			}
			if pErr.IsRetryable() {
				t.Error("IsRetryable() = true, want false")
			}
			if n := calls.Load(); n != 1 {
				t.Errorf("attempts = %d, want 1", n)
			}
		})
	}
}

func TestRetry_rateLimitExceededIsRetried(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := failingServer(t, 429, polipage.ErrCodeRateLimitExceeded, nil, &calls)
	client := newTestClient(t, server)
	_, err := client.Render.Document(context.Background(), minimalInput())
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || !pErr.IsRetryable() {
		t.Fatalf("err = %v, want a retryable RATE_LIMIT_EXCEEDED", err)
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("attempts = %d, want 3 (default maxRetries 2)", n)
	}
}

// firstRetryDelay returns the DelayMs of the first OnRetry event for a 503 carrying headers,
// and cancels the call there.
func firstRetryDelay(t *testing.T, headers map[string]string, retryDelay time.Duration) float64 {
	t.Helper()
	var calls atomic.Int32
	server := failingServer(t, 503, polipage.ErrCodePublishInProgress, headers, &calls)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var delays []float64
	client := newTestClient(t, server,
		option.WithRetryDelay(retryDelay),
		option.WithOnRetry(func(e polipage.RetryEvent) {
			mu.Lock()
			defer mu.Unlock()
			delays = append(delays, e.DelayMs)
			cancel()
		}),
	)
	_, err := client.Render.Document(ctx, minimalInput())
	if !errors.Is(err, polipage.ErrAborted) {
		t.Fatalf("err = %v, want aborted (canceled at the first retry)", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(delays) != 1 {
		t.Fatalf("retry events = %v, want 1", delays)
	}
	return delays[0]
}

func TestRetry_retryAfterZeroRetriesAtOnce(t *testing.T) {
	t.Parallel()
	if d := firstRetryDelay(t, map[string]string{"Retry-After": "0"}, time.Second); d != 0 {
		t.Errorf("DelayMs = %v, want 0 (Retry-After: 0, no backoff)", d)
	}
}

func TestRetry_hugeRetryAfterClampsTo30s(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"120", "99999999999999999999999"} {
		if d := firstRetryDelay(t, map[string]string{"Retry-After": value}, time.Second); d != 30000 {
			t.Errorf("Retry-After %s: DelayMs = %v, want 30000", value, d)
		}
	}
}

func TestRetry_retryAfterMsIsIgnored(t *testing.T) {
	t.Parallel()
	// Base delay 1 s: the first retry waits 500-1500 ms, never the header's 5 ms.
	d := firstRetryDelay(t, map[string]string{"Retry-After-Ms": "5"}, time.Second)
	if d < 500 || d >= 1500 {
		t.Errorf("DelayMs = %v, want the backoff in [500, 1500)", d)
	}
}

func TestRetry_nonIntegerRetryAfterFallsBackToBackoff(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"1.5", "-1", "soon"} {
		d := firstRetryDelay(t, map[string]string{"Retry-After": value}, time.Second)
		if d < 500 || d >= 1500 {
			t.Errorf("Retry-After %q: DelayMs = %v, want the backoff in [500, 1500)", value, d)
		}
	}
}

func TestErrors_sdkMessagesAreTheSpecTexts(t *testing.T) {
	t.Parallel()
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the body first: only then does the server notice the client going away.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	t.Cleanup(hang.Close)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "<html>502 Bad Gateway</html>")
	}))
	t.Cleanup(gateway.Close)
	// A listener closed at once: connecting to it fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedURL := "http://" + ln.Addr().String()
	_ = ln.Close()

	cases := []struct {
		name    string
		client  *polipage.Client
		input   polipage.RenderInput
		code    string
		message string
		status  int
		cause   bool
	}{
		{
			name:    "invalid_options",
			client:  polipage.NewClient(option.WithAPIKey("")),
			input:   minimalInput(),
			code:    polipage.ErrCodeInvalidOptions,
			message: "apiKey is required",
		},
		{
			name:    "network_error",
			client:  polipage.NewClient(option.WithAPIKey("pp_sandbox_x"), option.WithBaseURL(closedURL), option.WithMaxRetries(0)),
			input:   minimalInput(),
			code:    polipage.ErrCodeNetworkError,
			message: "Network request failed",
			cause:   true,
		},
		{
			name:    "timeout",
			client:  newTestClient(t, hang, option.WithTimeout(150*time.Millisecond), option.WithMaxRetries(0)),
			input:   minimalInput(),
			code:    polipage.ErrCodeTimeout,
			message: "Request timed out after 150ms",
			cause:   true,
		},
		{
			name:    "non-JSON error body",
			client:  newTestClient(t, gateway, option.WithMaxRetries(0)),
			input:   minimalInput(),
			code:    polipage.ErrCodeInternalError,
			message: "HTTP 502",
			status:  502,
		},
		{
			name:    "PROJECT_REQUIRED_FOR_DOCUMENT",
			client:  polipage.NewClient(option.WithAPIKey("pp_sandbox_x")),
			input:   polipage.RenderInput{Template: "t", Data: map[string]any{}},
			code:    polipage.ErrCodeProjectRequiredForDocument,
			message: "project is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.client.Render.Document(context.Background(), tc.input)
			var pErr *polipage.Error
			if !errors.As(err, &pErr) {
				t.Fatalf("err = %v (%T), want *Error", err, err)
			}
			if pErr.Code != tc.code || pErr.Message != tc.message || pErr.StatusCode != tc.status {
				t.Errorf("err = {%s %q %d}, want {%s %q %d}", pErr.Code, pErr.Message, pErr.StatusCode, tc.code, tc.message, tc.status)
			}
			if tc.cause && (pErr.Cause == nil || errors.Unwrap(err) != pErr.Cause) {
				t.Errorf("Cause = %v, want the transport error, reachable through Unwrap", pErr.Cause)
			}
		})
	}
}
