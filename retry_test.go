package polipage_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// staleConnServer answers the first request, then closes the connection of the second one
// after reading it (a keep-alive connection that dies before any answer), then answers again.
type staleConnServer struct {
	*httptest.Server
	mu    sync.Mutex
	addrs []string
}

func newStaleConnServer(t *testing.T) *staleConnServer {
	t.Helper()
	s := &staleConnServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		s.mu.Lock()
		s.addrs = append(s.addrs, r.RemoteAddr)
		n := len(s.addrs)
		s.mu.Unlock()
		if n == 2 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, canonicalDescriptorJSON())
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *staleConnServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.addrs...)
}

// TestRetry_postIsNeverResentByTheTransport checks that a POST whose reused keep-alive
// connection dies is not silently resent by net/http: every request the server sees is an SDK
// attempt, reported to OnRequest and counted by maxRetries.
func TestRetry_postIsNeverResentByTheTransport(t *testing.T) {
	t.Parallel()
	for _, maxRetries := range []int{0, 2} {
		t.Run("maxRetries="+strconv.Itoa(maxRetries), func(t *testing.T) {
			t.Parallel()
			server := newStaleConnServer(t)
			var attempts, retries atomic.Int32
			client := newTestClient(t, server.Server,
				option.WithMaxRetries(maxRetries),
				option.WithOnRequest(func(polipage.RequestEvent) { attempts.Add(1) }),
				option.WithOnRetry(func(polipage.RetryEvent) { retries.Add(1) }),
			)
			if _, err := client.Render.Document(context.Background(), minimalInput()); err != nil {
				t.Fatalf("first call: %v", err)
			}
			_, err := client.Render.Document(context.Background(), minimalInput())

			got := server.requests()
			if len(got) < 2 || got[1] != got[0] {
				t.Fatalf("server saw %v: the second request did not reuse the first connection, so the test proves nothing", got)
			}
			if n := int(attempts.Load()); len(got) != n {
				t.Errorf("server saw %d requests for %d SDK attempts: the transport resent a POST", len(got), n)
			}
			if maxRetries == 0 {
				var pErr *polipage.Error
				if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeNetworkError {
					t.Errorf("second call err = %v, want network_error (no retry allowed)", err)
				}
				if len(got) != 2 {
					t.Errorf("server saw %d requests, want 2", len(got))
				}
			} else {
				if err != nil {
					t.Errorf("second call err = %v, want success after the SDK's own retry", err)
				}
				if n := retries.Load(); n != 1 {
					t.Errorf("OnRetry fired %d times, want 1", n)
				}
			}
		})
	}
}

// partialAnswer answers 200 with a JSON descriptor announced at full length but cut after a
// few bytes, then either closes the connection (drop) or stalls until the client goes away.
// Requests after the first n get the full descriptor.
func partialAnswer(t *testing.T, n int32, drop bool, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	body := canonicalDescriptorJSON()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) > n {
			_, _ = io.WriteString(w, body)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body[:10])
		w.(http.Flusher).Flush()
		if drop {
			return // the server closes the connection: the body ends early
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRetry_answerBodyDroppedMidReadIsNetworkError(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := partialAnswer(t, 1, true, &calls)
	client := newTestClient(t, server, option.WithMaxRetries(0))
	_, err := client.Documents.Get(context.Background(), "doc_1")
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		t.Fatalf("err = %v (%T), want *Error", err, err)
	}
	if pErr.Code != polipage.ErrCodeNetworkError || pErr.Message != "Network request failed" || pErr.StatusCode != 0 {
		t.Errorf("err = {%s %q %d}, want {network_error \"Network request failed\" 0}", pErr.Code, pErr.Message, pErr.StatusCode)
	}
	if !pErr.IsRetryable() || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("IsRetryable() = %v, errors.Is(io.ErrUnexpectedEOF) = %v; want both true", pErr.IsRetryable(), errors.Is(err, io.ErrUnexpectedEOF))
	}
}

func TestRetry_answerBodyTimeoutMidReadIsTimeout(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := partialAnswer(t, 1, false, &calls)
	client := newTestClient(t, server, option.WithMaxRetries(0), option.WithTimeout(150*time.Millisecond))
	_, err := client.Documents.Get(context.Background(), "doc_1")
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		t.Fatalf("err = %v (%T), want *Error", err, err)
	}
	if pErr.Code != polipage.ErrCodeTimeout || pErr.Message != "Request timed out after 150ms" || pErr.StatusCode != 0 {
		t.Errorf("err = {%s %q %d}, want {timeout \"Request timed out after 150ms\" 0}", pErr.Code, pErr.Message, pErr.StatusCode)
	}
	if !pErr.IsRetryable() {
		t.Error("IsRetryable() = false, want true")
	}
}

func TestRetry_answerBodyDroppedMidReadIsRetried(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var attempts, retries, responses atomic.Int32
	server := partialAnswer(t, 1, true, &calls)
	client := newTestClient(t, server,
		option.WithOnRequest(func(polipage.RequestEvent) { attempts.Add(1) }),
		option.WithOnRetry(func(polipage.RetryEvent) { retries.Add(1) }),
		option.WithOnResponse(func(polipage.ResponseEvent) { responses.Add(1) }),
	)
	doc, err := client.Documents.Get(context.Background(), "doc_1")
	if err != nil {
		t.Fatalf("err = %v, want success on the second attempt", err)
	}
	if doc.DocumentID == "" {
		t.Error("empty descriptor")
	}
	if a, r, o, c := attempts.Load(), retries.Load(), responses.Load(), calls.Load(); a != 2 || r != 1 || o != 1 || c != 2 {
		t.Errorf("attempts=%d retries=%d responses=%d server calls=%d, want 2, 1, 1, 2", a, r, o, c)
	}
}
