package polipage_test

import (
	"bytes"
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

// twoHopServer mounts two routes on a single httptest.Server:
//   - POST /v1/render returns a JSON DocumentDescriptor whose
//     presignedPdfUrl points at GET /s3/<docID>.pdf on the same server.
//   - GET /s3/* returns the supplied pdfBytes verbatim.
//
// onRender (optional) sees the parsed body of the POST so individual tests
// can assert on the wire payload without re-implementing the handler.
func twoHopServer(t *testing.T, pdfBytes []byte, onRender func(body []byte)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/v1/render", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if onRender != nil {
			onRender(body)
		}
		descriptor := map[string]any{
			"documentId":      "doc_test_abc",
			"organizationId":  "org_test_xyz",
			"projectId":       "proj_001",
			"projectSlug":     "billing",
			"templateId":      "tmpl_001",
			"templateSlug":    "invoice",
			"version":         "1.0.0",
			"environment":     "sandbox",
			"apiKeyId":        "key_001",
			"format":          "A4",
			"orientation":     "portrait",
			"locale":          "en-US",
			"pageCount":       2,
			"sizeBytes":       len(pdfBytes),
			"createdAt":       "2026-05-21T10:00:00Z",
			"metadata":        map[string]any{"source": "test"},
			"presignedPdfUrl": serverURL + "/s3/doc_test_abc.pdf",
			"expiresAt":       "2026-05-21T10:15:00Z",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(descriptor)
	})
	mux.HandleFunc("/s3/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(pdfBytes)
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)
	return server
}

func TestRender_Document_happyPath(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-1.4\nfake pdf bytes\n%%EOF")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)

	doc, err := client.Render.Document(context.Background(), polipage.ProjectModeInput{
		Project: "billing", Template: "invoice",
		Version: polipage.Opt("1.0.0"),
		Data:    map[string]any{"invoiceNumber": "INV-001"},
	})
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	if doc.DocumentID != "doc_test_abc" {
		t.Errorf("DocumentID = %q, want doc_test_abc", doc.DocumentID)
	}
	if doc.OrganizationID != "org_test_xyz" {
		t.Errorf("OrganizationID = %q, want org_test_xyz", doc.OrganizationID)
	}
	if doc.Environment != polipage.EnvironmentSandbox {
		t.Errorf("Environment = %q, want sandbox", doc.Environment)
	}
	if doc.PageCount != 2 || doc.SizeBytes != int64(len(pdf)) {
		t.Errorf("PageCount=%d SizeBytes=%d, want 2/%d", doc.PageCount, doc.SizeBytes, len(pdf))
	}
	if doc.PresignedPDFURL == "" || !strings.Contains(doc.PresignedPDFURL, "/s3/") {
		t.Errorf("PresignedPDFURL = %q, want server-relative s3 URL", doc.PresignedPDFURL)
	}
	// Nullable wire fields decoded to *string.
	if doc.ProjectSlug == nil || *doc.ProjectSlug != "billing" {
		t.Errorf("ProjectSlug = %v, want non-nil pointing to 'billing'", doc.ProjectSlug)
	}
	if doc.Version == nil || *doc.Version != "1.0.0" {
		t.Errorf("Version = %v, want non-nil pointing to '1.0.0'", doc.Version)
	}
}

func TestRender_Document_preservesNullsAsNilPointers(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"documentId":"doc_1","organizationId":"org_1",
			"projectId":null,"projectSlug":null,"templateId":null,"templateSlug":null,
			"version":null,"environment":"sandbox","apiKeyId":null,
			"format":"A4","orientation":null,"locale":null,
			"pageCount":1,"sizeBytes":100,"createdAt":"2026-05-21T10:00:00Z",
			"metadata":{},"presignedPdfUrl":"https://s3.example/x","expiresAt":"2026-05-21T10:15:00Z"
		}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	doc, err := client.Render.Document(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	if doc.ProjectID != nil || doc.ProjectSlug != nil || doc.Version != nil || doc.Orientation != nil || doc.Locale != nil || doc.APIKeyID != nil {
		t.Errorf("expected nullable fields to be nil pointers, got %+v", doc)
	}
}

func TestRender_Document_metadataAlwaysNonNil(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"documentId":"d","organizationId":"o","environment":"sandbox",
			"format":"A4","pageCount":1,"sizeBytes":1,
			"createdAt":"2026-05-21T10:00:00Z",
			"metadata":null,
			"presignedPdfUrl":"https://x/x","expiresAt":"2026-05-21T10:15:00Z"
		}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	doc, err := client.Render.Document(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	if doc.Metadata == nil {
		t.Fatal("Metadata = nil, want non-nil empty map (server sent null)")
	}
	if len(doc.Metadata) != 0 {
		t.Errorf("Metadata = %v, want empty map", doc.Metadata)
	}
}

func TestRender_Document_emptyProjectRejectedClientSide(t *testing.T) {
	t.Parallel()
	// Defense in depth: an empty Project must fail before any HTTP call
	// with ErrCodeProjectRequiredForDocument.
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Render.Document(context.Background(), polipage.ProjectModeInput{
		Project: "", Template: "y", Data: map[string]any{},
	})
	if err == nil {
		t.Fatal("Document err = nil, want validation error")
	}
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeProjectRequiredForDocument {
		t.Errorf("err = %v, want Code=PROJECT_REQUIRED_FOR_DOCUMENT", err)
	}
	if calls.Load() != 0 {
		t.Error("server received a request; client-side validation should have prevented it")
	}
}

func TestRender_Document_metadataNestedValueRejected(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Render.Document(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
		Metadata: polipage.RenderMetadata{
			"nested": map[string]any{"oops": "no nested maps"},
		},
	})
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeInvalidOptions {
		t.Errorf("err = %v, want *Error with Code=invalid_options", err)
	}
	if calls.Load() != 0 {
		t.Error("server received a request; metadata validation should be pre-flight")
	}
}

func TestRender_PDF_twoHopReturnsBytes(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-1.4\nthe content\n%%EOF")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)

	got, err := client.Render.PDF(context.Background(), polipage.ProjectModeInput{
		Project: "billing", Template: "invoice",
		Version: polipage.Opt("1.0.0"),
		Data:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("PDF err = %v", err)
	}
	if !bytes.Equal(got, pdf) {
		t.Fatalf("PDF bytes mismatch:\n got %q\nwant %q", got, pdf)
	}
}

func TestRender_PDF_presignedURLFailureMapsToDownloadFailed(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/v1/render", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"documentId":"d","organizationId":"o","environment":"sandbox",
			"format":"A4","pageCount":1,"sizeBytes":1,
			"createdAt":"2026-05-21T10:00:00Z","metadata":{},
			"presignedPdfUrl":"`+serverURL+`/s3/expired",
			"expiresAt":"2026-05-21T10:15:00Z"
		}`)
	})
	mux.HandleFunc("/s3/expired", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	client := newTestClient(t, server)
	_, err := client.Render.PDF(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeDownloadFailed {
		t.Fatalf("err = %v, want Code=DOWNLOAD_FAILED", err)
	}
	if pErr.StatusCode != 403 {
		t.Errorf("StatusCode = %d, want 403", pErr.StatusCode)
	}
}

func TestRender_PDFStream_returnsReadableBody(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-stream content")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)

	body, err := client.Render.PDFStream(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err != nil {
		t.Fatalf("PDFStream err = %v", err)
	}
	defer func() { _ = body.Close() }()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body err = %v", err)
	}
	if !bytes.Equal(got, pdf) {
		t.Fatalf("stream bytes mismatch:\n got %q\nwant %q", got, pdf)
	}
}

func TestDocumentDescriptor_DownloadPDF_succeeds(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-from-descriptor")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)

	doc, err := client.Render.Document(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	got, err := doc.DownloadPDF(context.Background())
	if err != nil {
		t.Fatalf("DownloadPDF err = %v", err)
	}
	if !bytes.Equal(got, pdf) {
		t.Fatalf("download bytes mismatch:\n got %q\nwant %q", got, pdf)
	}
}

func TestRender_Preview_perCallWithRequestTimeoutOverridesClient(t *testing.T) {
	t.Parallel()
	// Slow server: 1s response. Client timeout is generous (60s default),
	// but the per-call WithRequestTimeout of 50ms must fire first.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(1 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	client := newTestClient(t, server, option.WithMaxRetries(0))
	_, err := client.Render.Preview(
		context.Background(),
		polipage.ProjectModeInput{Project: "x", Template: "y", Data: map[string]any{}},
		option.WithRequestTimeout(50*time.Millisecond),
	)
	if !errors.Is(err, polipage.ErrTimeout) {
		t.Fatalf("err = %v, want errors.Is(..., ErrTimeout)", err)
	}
}

func TestRender_Preview_perCallWithHeaderAttachesHeader(t *testing.T) {
	t.Parallel()
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Trace-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Render.Preview(
		context.Background(),
		polipage.ProjectModeInput{Project: "x", Template: "y", Data: map[string]any{}},
		option.WithHeader("X-Trace-Id", "trace-abc-123"),
	)
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if got != "trace-abc-123" {
		t.Errorf("X-Trace-Id = %q, want trace-abc-123", got)
	}
}

func TestRender_Preview_constructionWithHeaderAppliesToEveryRequest(t *testing.T) {
	t.Parallel()
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Tenant"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	client := newTestClient(t, server, option.WithHeader("X-Tenant", "tenant-42"))
	for i := 0; i < 2; i++ {
		if _, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
			Project: "x", Template: "y", Data: map[string]any{},
		}); err != nil {
			t.Fatalf("Preview err = %v", err)
		}
	}
	for i, h := range seen {
		if h != "tenant-42" {
			t.Errorf("seen[%d] = %q, want tenant-42", i, h)
		}
	}
}

func TestRender_Preview_perCallHeaderOverridesConstructionHeader(t *testing.T) {
	t.Parallel()
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Env")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, renderPreviewJSON)
	}))
	defer server.Close()

	client := newTestClient(t, server, option.WithHeader("X-Env", "production"))
	_, err := client.Render.Preview(
		context.Background(),
		polipage.ProjectModeInput{Project: "x", Template: "y", Data: map[string]any{}},
		option.WithHeader("X-Env", "staging"), // per-call wins
	)
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if got != "staging" {
		t.Errorf("X-Env = %q, want staging (per-call overrides construction)", got)
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
