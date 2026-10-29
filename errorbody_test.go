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
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
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

type hookCounts struct {
	attempts, retries atomic.Int32
}

func (h *hookCounts) options() []option.RequestOption {
	return []option.RequestOption{
		option.WithOnRequest(func(polipage.RequestEvent) { h.attempts.Add(1) }),
		option.WithOnRetry(func(polipage.RetryEvent) { h.retries.Add(1) }),
	}
}

func TestErrorBody_cancelWhileReadingIsAborted(t *testing.T) {
	t.Parallel()
	for _, status := range []int{400, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			var hooks hookCounts
			server := cutErrorBody(t, status, false, &calls)
			client := newTestClient(t, server, hooks.options()...)
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(100*time.Millisecond, cancel)
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

// wantMalformedBody checks a failed error-body read: a non-JSON body (§8.2), the real status,
// the read error as Cause.
func wantMalformedBody(t *testing.T, err error, status int) {
	t.Helper()
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		t.Fatalf("err = %v (%T), want *Error", err, err)
	}
	want := "HTTP " + strconv.Itoa(status)
	if pErr.Code != polipage.ErrCodeInternalError || pErr.Message != want || pErr.StatusCode != status {
		t.Errorf("err = {%s %q %d}, want {INTERNAL_ERROR %q %d}", pErr.Code, pErr.Message, pErr.StatusCode, want, status)
	}
	if pErr.Cause == nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Cause = %v, want the read error (io.ErrUnexpectedEOF)", pErr.Cause)
	}
}

func TestErrorBody_droppedDuring400IsMalformedAndNotRetried(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var hooks hookCounts
	server := cutErrorBody(t, 400, true, &calls)
	client := newTestClient(t, server, hooks.options()...)
	_, err := client.Documents.Get(context.Background(), "doc_1")
	wantMalformedBody(t, err, 400)
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
	wantMalformedBody(t, err, 503)
	var pErr *polipage.Error
	if errors.As(err, &pErr) && !pErr.IsRetryable() {
		t.Error("IsRetryable() = false, want true for a 503")
	}
	if a, r, c := hooks.attempts.Load(), hooks.retries.Load(), calls.Load(); a != 3 || r != 2 || c != 3 {
		t.Errorf("attempts=%d retries=%d server calls=%d, want 3, 2, 3 (default maxRetries 2)", a, r, c)
	}
}
