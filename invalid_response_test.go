package polipage_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// htmlServer answers every request with status and an HTML page, the way a
// proxy or a captive portal between the SDK and the API would, and counts
// the requests.
func htmlServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Request-Id", "req_portal")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func wantInvalidResponse(t *testing.T, err error, status int) {
	t.Helper()
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeInvalidResponse {
		t.Fatalf("err = %v (%T), want *Error with Code=invalid_response", err, err)
	}
	if !errors.Is(err, polipage.ErrInvalidResponse) {
		t.Errorf("errors.Is(err, ErrInvalidResponse) = false")
	}
	want := "Invalid response: HTTP " + strconv.Itoa(status) + " body is not JSON"
	if pErr.Message != want || pErr.StatusCode != status || pErr.RequestID != "req_portal" {
		t.Errorf("err = %+v, want message %q, status %d and request id req_portal", pErr, want, status)
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Errorf("Cause = %v, want the *json.SyntaxError", pErr.Cause)
	}
	if pErr.IsRetryable() || pErr.IsNetworkError() {
		t.Errorf("IsRetryable() = %v, IsNetworkError() = %v; want false, false", pErr.IsRetryable(), pErr.IsNetworkError())
	}
	payload := pErr.ToPayload()
	if payload.Status != 502 || pErr.StatusCode != status {
		t.Errorf("ToPayload().Status = %d, StatusCode = %d; want 502 and the real %d", payload.Status, pErr.StatusCode, status)
	}
}

func TestInvalidResponse_eachJSONMethod(t *testing.T) {
	t.Parallel()
	page := "<html><body><h1>Welcome</h1></body></html>"
	calls := map[string]func(*polipage.Client) error{
		"Render.Document": func(c *polipage.Client) error {
			_, err := c.Render.Document(context.Background(), minimalInput())
			return err
		},
		"Render.PDF": func(c *polipage.Client) error {
			_, err := c.Render.PDF(context.Background(), minimalInput())
			return err
		},
		"Render.PDFStream": func(c *polipage.Client) error {
			s, err := c.Render.PDFStream(context.Background(), minimalInput())
			if err == nil {
				_ = s.Close()
			}
			return err
		},
		"Documents.Get": func(c *polipage.Client) error {
			_, err := c.Documents.Get(context.Background(), "doc_1")
			return err
		},
		"Documents.Thumbnails": func(c *polipage.Client) error {
			_, err := c.Documents.Thumbnails(context.Background(), "doc_1", polipage.ThumbnailOptions{})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server, requests := htmlServer(t, http.StatusOK, page)
			var hooked atomic.Int32
			client := newTestClient(t, server, option.WithOnError(func(err error) {
				if errors.Is(err, polipage.ErrInvalidResponse) {
					hooked.Add(1)
				}
			}))
			wantInvalidResponse(t, call(client), http.StatusOK)
			if n := requests.Load(); n != 1 {
				t.Errorf("requests = %d, want 1: invalid_response is never retried", n)
			}
			if n := hooked.Load(); n != 1 {
				t.Errorf("OnError calls with invalid_response = %d, want 1", n)
			}
		})
	}
}

func TestInvalidResponse_emptyBodyAndOther2xx(t *testing.T) {
	t.Parallel()
	server, _ := htmlServer(t, http.StatusCreated, "")
	client := newTestClient(t, server)
	_, err := client.Documents.Get(context.Background(), "doc_1")
	wantInvalidResponse(t, err, http.StatusCreated)
}

func TestInvalidResponse_validJSONOfTheWrongShapeStaysInternalError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `["not", "a", "descriptor"]`)
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server)
	_, err := client.Documents.Get(context.Background(), "doc_1")
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeInternalError || pErr.StatusCode != http.StatusOK {
		t.Fatalf("err = %v, want INTERNAL_ERROR with status 200: the body is JSON", err)
	}
}

func TestInvalidResponse_previewAndDeleteExpectNoJSON(t *testing.T) {
	t.Parallel()
	server, _ := htmlServer(t, http.StatusOK, "<html>page</html>")
	client := newTestClient(t, server)
	if _, err := client.Documents.Preview(context.Background(), "doc_1"); err != nil {
		t.Errorf("Preview err = %v, want nil: its body is HTML", err)
	}
	if err := client.Documents.Delete(context.Background(), "doc_1"); err != nil {
		t.Errorf("Delete err = %v, want nil: its body is ignored", err)
	}
}
