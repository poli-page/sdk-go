package polipage_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// renderPreviewJSON is a canned 200 OK body matching the deployed API's
// PreviewResult shape (html / totalPages / environment).
const renderPreviewJSON = `{"html":"<p>hello</p>","totalPages":3,"environment":"sandbox"}`

func newTestClient(t *testing.T, server *httptest.Server, extra ...option.RequestOption) *polipage.Client {
	t.Helper()
	opts := append([]option.RequestOption{
		option.WithAPIKey("pp_test_xyz"),
		option.WithBaseURL(server.URL),
		option.WithHTTPClient(server.Client()),
		option.WithRetryDelay(1 * time.Millisecond), // keep retry tests fast
	}, extra...)
	return polipage.NewClient(opts...)
}

func TestRender_Preview_happyPath(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/render/preview" {
			t.Errorf("path = %s, want /v1/render/preview", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer pp_test_xyz" {
			t.Errorf("Authorization = %q, want Bearer pp_test_xyz", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	got, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project:  "billing",
		Template: "invoice",
		Version:  polipage.Opt("1.0.0"),
		Data:     map[string]any{"invoiceNumber": "INV-001"},
	})
	if err != nil {
		t.Fatalf("Preview err = %v, want nil", err)
	}
	if got.HTML != "<p>hello</p>" || got.TotalPages != 3 || got.Environment != polipage.EnvironmentSandbox {
		t.Fatalf("Preview = %+v, want {HTML:<p>hello</p>, TotalPages:3, Environment:sandbox}", got)
	}
}

func TestRender_Preview_sendsRequestBodyAsCamelCaseJSON(t *testing.T) {
	t.Parallel()
	var bodySeen json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodySeen = b
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project:  "billing",
		Template: "invoice",
		Version:  polipage.Opt("1.0.0"),
		Data:     map[string]any{"invoiceNumber": "INV-001"},
		Locale:   "fr-FR",
	})
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	got := string(bodySeen)
	for _, want := range []string{`"project":"billing"`, `"template":"invoice"`, `"version":"1.0.0"`, `"locale":"fr-FR"`, `"invoiceNumber":"INV-001"`} {
		if !strings.Contains(got, want) {
			t.Errorf("body = %s\nwant to contain %s", got, want)
		}
	}
}

func TestRender_Preview_inlineModeAccepted(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Render.Preview(context.Background(), polipage.InlineModeInput{
		Template: "<html>{{name}}</html>",
		Data:     map[string]any{"name": "Mickael"},
	})
	if err != nil {
		t.Fatalf("Preview(InlineModeInput) err = %v", err)
	}
}

func TestRender_Preview_4xxNoRetryReturnsTypedError(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_abc")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":"VALIDATION_ERROR","message":"data is required"}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err == nil {
		t.Fatal("Preview err = nil, want *Error")
	}
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		t.Fatalf("errors.As(err, *Error) failed; err = %v", err)
	}
	if pErr.StatusCode != 400 || pErr.Code != "VALIDATION_ERROR" || pErr.RequestID != "req_abc" {
		t.Errorf("err = %+v, want status=400 code=VALIDATION_ERROR requestID=req_abc", pErr)
	}
	if !errors.Is(err, polipage.ErrValidation) {
		t.Error("errors.Is(err, ErrValidation) = false, want true")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestRender_Preview_5xxRetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	client := newTestClient(t, server, option.WithMaxRetries(3))
	res, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if res.HTML == "" {
		t.Error("Preview result empty after retry success")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (two 502s then success)", got)
	}
}

func TestRender_Preview_5xxRetriesExhausted(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(t, server, option.WithMaxRetries(2))
	_, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err == nil {
		t.Fatal("Preview err = nil, want *Error")
	}
	if got := calls.Load(); got != 3 { // initial + 2 retries
		t.Errorf("calls = %d, want 3 (initial + MaxRetries=2)", got)
	}
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.StatusCode != 500 {
		t.Errorf("err = %v, want *Error with StatusCode=500", err)
	}
}

func TestRender_Preview_autoGeneratesIdempotencyKeyUUID4(t *testing.T) {
	t.Parallel()
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if len(got) != 36 || got[8] != '-' || got[14] != '4' {
		t.Errorf("Idempotency-Key = %q, want UUID4-shaped", got)
	}
}

func TestRender_Preview_customIdempotencyKeyOverridesAuto(t *testing.T) {
	t.Parallel()
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Render.Preview(
		context.Background(),
		polipage.ProjectModeInput{Project: "x", Template: "y", Data: map[string]any{}},
		option.WithIdempotencyKey("inv-INV-001"),
	)
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if got != "inv-INV-001" {
		t.Errorf("Idempotency-Key = %q, want inv-INV-001", got)
	}
}

func TestRender_Preview_ctxCancelMapsToAborted(t *testing.T) {
	t.Parallel()
	// Slow server so the cancel races the response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	client := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := client.Render.Preview(ctx, polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if !errors.Is(err, polipage.ErrAborted) {
		t.Fatalf("err = %v, want errors.Is(..., ErrAborted)", err)
	}
}

func TestNewClient_emptyAPIKeyReturnsErrorOnFirstCall(t *testing.T) {
	t.Parallel()
	// No WithAPIKey: NewClient still returns *Client, but the first method
	// call surfaces *Error{Code: ErrCodeInvalidOptions}.
	client := polipage.NewClient(option.WithBaseURL("https://api.poli.page"))
	_, err := client.Render.Preview(context.Background(), polipage.InlineModeInput{
		Template: "<html/>", Data: map[string]any{},
	})
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		t.Fatalf("err = %v, want *Error", err)
	}
	if pErr.Code != polipage.ErrCodeInvalidOptions {
		t.Errorf("err.Code = %q, want %q", pErr.Code, polipage.ErrCodeInvalidOptions)
	}
}

func TestRender_Preview_onRetryHookFires(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	var retryEvents []polipage.RetryEvent
	client := newTestClient(t, server, option.WithOnRetry(func(e polipage.RetryEvent) {
		retryEvents = append(retryEvents, e)
	}))
	_, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if len(retryEvents) != 1 {
		t.Fatalf("retry hook fired %d times, want 1", len(retryEvents))
	}
	if retryEvents[0].Attempt != 2 {
		t.Errorf("retry event attempt = %d, want 2", retryEvents[0].Attempt)
	}
	var pErr *polipage.Error
	if !errors.As(retryEvents[0].Reason, &pErr) || pErr.StatusCode != 503 {
		t.Errorf("retry event reason = %v, want *Error{StatusCode:503}", retryEvents[0].Reason)
	}
}

func TestRender_Preview_onErrorHookFiresOnceOnTerminalFailure(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"code":"FORBIDDEN","message":"nope"}`)
	}))
	defer server.Close()

	var errorCalls atomic.Int32
	client := newTestClient(t, server, option.WithOnError(func(error) { errorCalls.Add(1) }))
	_, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err == nil {
		t.Fatal("Preview err = nil, want *Error")
	}
	if got := errorCalls.Load(); got != 1 {
		t.Errorf("onError fired %d times, want 1", got)
	}
}

func TestRender_Preview_largeResponseBodyDoesNotRaceAttemptTimeout(t *testing.T) {
	t.Parallel()
	// Regression: when the response body is large enough that decode needs
	// multiple reads, the per-attempt timeout context must remain live until
	// the caller closes the body. Cancelling in sendOnce() killed body reads
	// for project-mode previews on the real API.
	bigHTML := strings.Repeat("<p>chunk</p>", 50_000) // ~600 KB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"html":        bigHTML,
			"totalPages":  10,
			"environment": "sandbox",
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	res, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if len(res.HTML) != len(bigHTML) {
		t.Errorf("HTML length = %d, want %d", len(res.HTML), len(bigHTML))
	}
}

func TestRender_Preview_hookPanicDoesNotBreakRequest(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	client := newTestClient(t, server,
		option.WithOnRetry(func(polipage.RetryEvent) { panic("boom") }),
		option.WithOnError(func(error) { panic("boom") }),
	)
	res, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Preview err = %v (hook panic should be recovered)", err)
	}
	if res.HTML == "" {
		t.Error("Preview returned empty result despite successful response")
	}
}
