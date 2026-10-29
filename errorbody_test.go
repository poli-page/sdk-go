package polipage_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// cutErrorBody answers status with an error body announced longer than it is. The bytes it
// does send are a complete JSON object, so a client that parses them anyway picks up a code
// and a message from a body it never received in full. Then the server closes the connection
// (drop) or stalls until the client goes away.
func cutErrorBody(t *testing.T, status int, drop bool, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	const sent = `{"error":"PUBLISH_IN_PROGRESS","detail":"from the partial body"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_partial_"+strconv.Itoa(status)+"_"+strconv.Itoa(int(n)))
		w.Header().Set("Content-Length", strconv.Itoa(len(sent)+100))
		w.WriteHeader(status)
		_, _ = io.WriteString(w, sent)
		w.(http.Flusher).Flush()
		if drop {
			return // the server closes the connection: the body ends early
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	return server
}

// cancelAfterHeaders cancels the call once a response's headers have arrived, before its body
// is read.
type cancelAfterHeaders struct {
	base   http.RoundTripper
	cancel context.CancelFunc
}

func (c cancelAfterHeaders) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.base.RoundTrip(req)
	if err == nil {
		c.cancel()
	}
	return resp, err
}

type hookCounts struct {
	attempts, retries, errors atomic.Int32
}

func (h *hookCounts) options() []option.RequestOption {
	return []option.RequestOption{
		option.WithOnRequest(func(polipage.RequestEvent) { h.attempts.Add(1) }),
		option.WithOnRetry(func(polipage.RetryEvent) { h.retries.Add(1) }),
		option.WithOnError(func(error) { h.errors.Add(1) }),
	}
}

func TestErrorBody_cancelWhileReadingIsAborted(t *testing.T) {
	t.Parallel()
	for _, status := range []int{400, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			var hooks hookCounts
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := cutErrorBody(t, status, false, &calls)
			// Cancel as soon as the answer's headers are back, so the cancel always
			// lands while the body is read (the server never sends the rest).
			httpClient := &http.Client{Transport: cancelAfterHeaders{base: server.Client().Transport, cancel: cancel}}
			client := newTestClient(t, server, append(hooks.options(), option.WithHTTPClient(httpClient))...)
			_, err := client.Documents.Get(ctx, "doc_1")
			var pErr *polipage.Error
			if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeAborted || pErr.Message != "Request was aborted" || pErr.StatusCode != 0 {
				t.Fatalf("err = %v, want aborted with no status", err)
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("errors.Is(err, context.Canceled) = false for %v", err)
			}
			// Aborted at once: no retry announced before the cancel is noticed.
			if a, r, c := hooks.attempts.Load(), hooks.retries.Load(), calls.Load(); a != 1 || r != 0 || c != 1 {
				t.Errorf("attempts=%d retries=%d server calls=%d, want 1, 0, 1", a, r, c)
			}
		})
	}
}

// wantMalformedBody checks a failed error-body read: a non-JSON body (§8.2), the real status
// and request id of the last attempt, the read error as Cause.
func wantMalformedBody(t *testing.T, err error, status, lastAttempt int) {
	t.Helper()
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		t.Fatalf("err = %v (%T), want *Error", err, err)
	}
	want := "HTTP " + strconv.Itoa(status)
	if pErr.Code != polipage.ErrCodeInternalError || pErr.Message != want || pErr.StatusCode != status {
		t.Errorf("err = {%s %q %d}, want {INTERNAL_ERROR %q %d}", pErr.Code, pErr.Message, pErr.StatusCode, want, status)
	}
	if want := "req_partial_" + strconv.Itoa(status) + "_" + strconv.Itoa(lastAttempt); pErr.RequestID != want {
		t.Errorf("RequestID = %q, want %q", pErr.RequestID, want)
	}
	if pErr.Cause == nil {
		t.Error("Cause = nil, want the read error")
	}
}

func TestErrorBody_droppedDuring400IsMalformedAndNotRetried(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var hooks hookCounts
	server := cutErrorBody(t, 400, true, &calls)
	client := newTestClient(t, server, hooks.options()...)
	_, err := client.Documents.Get(context.Background(), "doc_1")
	wantMalformedBody(t, err, 400, 1)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("errors.Is(err, io.ErrUnexpectedEOF) = false for %v", err)
	}
	if n := hooks.errors.Load(); n != 1 {
		t.Errorf("OnError fired %d times, want 1", n)
	}
	if a, r, c := hooks.attempts.Load(), hooks.retries.Load(), calls.Load(); a != 1 || r != 0 || c != 1 {
		t.Errorf("attempts=%d retries=%d server calls=%d, want 1, 0, 1", a, r, c)
	}
}

func TestErrorBody_droppedDuring503IsMalformedAndRetried(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var hooks hookCounts
	server := cutErrorBody(t, 503, true, &calls)
	client := newTestClient(t, server, hooks.options()...)
	_, err := client.Documents.Get(context.Background(), "doc_1")
	wantMalformedBody(t, err, 503, 3)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("errors.Is(err, io.ErrUnexpectedEOF) = false for %v", err)
	}
	var pErr *polipage.Error
	if errors.As(err, &pErr) && !pErr.IsRetryable() {
		t.Error("IsRetryable() = false, want true for a 503")
	}
	if a, r, c := hooks.attempts.Load(), hooks.retries.Load(), calls.Load(); a != 3 || r != 2 || c != 3 {
		t.Errorf("attempts=%d retries=%d server calls=%d, want 3, 2, 3 (default maxRetries 2)", a, r, c)
	}
}

// An attempt timeout is not a caller cancel: an error body that stalls past it keeps its
// status, is a malformed body and is retried by that status, never aborted.
func TestErrorBody_stalledPastAttemptTimeoutKeepsStatusAndIsRetried(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var hooks hookCounts
	server := cutErrorBody(t, 503, false, &calls)
	client := newTestClient(t, server, append(hooks.options(), option.WithTimeout(150*time.Millisecond))...)
	_, err := client.Documents.Get(context.Background(), "doc_1")
	wantMalformedBody(t, err, 503, 3)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(DeadlineExceeded) = %v, errors.Is(Canceled) = %v; want true, false",
			errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled))
	}
	var pErr *polipage.Error
	if errors.As(err, &pErr) && !pErr.IsRetryable() {
		t.Error("IsRetryable() = false, want true for a 503")
	}
	if a, r, c := hooks.attempts.Load(), hooks.retries.Load(), calls.Load(); a != 3 || r != 2 || c != 3 {
		t.Errorf("attempts=%d retries=%d server calls=%d, want 3, 2, 3 (default maxRetries 2)", a, r, c)
	}
}
