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

// canonicalDescriptorJSON returns a JSON descriptor body for tests that
// only care about the wire shape, not specific field values.
func canonicalDescriptorJSON() string {
	return `{
		"documentId":"doc_abc","organizationId":"org_xyz",
		"projectId":"p1","projectSlug":"billing","templateId":"t1","templateSlug":"invoice",
		"version":"1.0.0","environment":"sandbox","apiKeyId":"k1",
		"format":"A4","orientation":"portrait","locale":"en-US",
		"pageCount":2,"sizeBytes":4096,"createdAt":"2026-05-21T10:00:00Z",
		"metadata":{"source":"test"},
		"presignedPdfUrl":"https://s3.example/doc_abc.pdf",
		"expiresAt":"2026-05-21T10:15:00Z"
	}`
}

func TestDocuments_Get_happyPath(t *testing.T) {
	t.Parallel()
	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, canonicalDescriptorJSON())
	}))
	defer server.Close()

	client := newTestClient(t, server)
	doc, err := client.Documents.Get(context.Background(), "doc_abc")
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	if seenPath != "/v1/documents/doc_abc" {
		t.Errorf("path = %q, want /v1/documents/doc_abc", seenPath)
	}
	if doc.DocumentID != "doc_abc" || doc.PageCount != 2 {
		t.Errorf("doc = %+v, want DocumentID=doc_abc PageCount=2", doc)
	}
	// Back-reference wired: DownloadPDF must be usable from a Documents.Get result.
	if doc.PresignedPDFURL == "" {
		t.Error("PresignedPDFURL empty")
	}
}

func TestDocuments_Get_404MapsToErrNotFound(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"code":"DOCUMENT_NOT_FOUND","message":"no such doc"}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Documents.Get(context.Background(), "doc_missing")
	if !errors.Is(err, polipage.ErrDocumentNotFound) {
		t.Fatalf("err = %v, want errors.Is(..., ErrDocumentNotFound)", err)
	}
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.StatusCode != 404 {
		t.Errorf("err = %v, want StatusCode=404", err)
	}
}

func TestDocuments_Get_URLEncodesID(t *testing.T) {
	t.Parallel()
	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, canonicalDescriptorJSON())
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Documents.Get(context.Background(), "doc with/slash")
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	if !strings.Contains(seenPath, "doc%20with%2Fslash") {
		t.Errorf("escaped path = %q, want to contain doc%%20with%%2Fslash", seenPath)
	}
}

func TestDocuments_Preview_returnsHTMLAndPageCountFromHeader(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/preview") {
			t.Errorf("path = %s, want trailing /preview", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Document-Page-Count", "7")
		_, _ = io.WriteString(w, "<html><body>stored preview</body></html>")
	}))
	defer server.Close()

	client := newTestClient(t, server)
	got, err := client.Documents.Preview(context.Background(), "doc_abc")
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if got.PageCount != 7 {
		t.Errorf("PageCount = %d, want 7", got.PageCount)
	}
	if !strings.Contains(got.HTML, "stored preview") {
		t.Errorf("HTML = %q, want to contain 'stored preview'", got.HTML)
	}
}

func TestDocuments_Preview_missingHeaderYieldsZeroPageCount(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<p>no header</p>")
	}))
	defer server.Close()

	client := newTestClient(t, server)
	got, err := client.Documents.Preview(context.Background(), "doc_abc")
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if got.PageCount != 0 {
		t.Errorf("PageCount = %d, want 0 (header absent)", got.PageCount)
	}
	if got.HTML != "<p>no header</p>" {
		t.Errorf("HTML = %q, want '<p>no header</p>'", got.HTML)
	}
}

func TestDocuments_Preview_unparseableHeaderYieldsZeroPageCount(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Document-Page-Count", "not-a-number")
		_, _ = io.WriteString(w, "<p>x</p>")
	}))
	defer server.Close()

	client := newTestClient(t, server)
	got, err := client.Documents.Preview(context.Background(), "doc_abc")
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if got.PageCount != 0 {
		t.Errorf("PageCount = %d, want 0 (header unparseable)", got.PageCount)
	}
}

func TestDocuments_Thumbnails_wrapsBodyAndUnwrapsResponse(t *testing.T) {
	t.Parallel()
	var bodySeen json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/thumbnails") {
			t.Errorf("path = %s, want /thumbnails suffix", r.URL.Path)
		}
		bodySeen, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"thumbnails":[
			{"page":1,"width":840,"height":1188,"contentType":"image/png","data":"AAAA"},
			{"page":2,"width":840,"height":1188,"contentType":"image/png","data":"BBBB"}
		]}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	thumbs, err := client.Documents.Thumbnails(context.Background(), "doc_abc", polipage.ThumbnailOptions{
		Width:  840,
		Format: polipage.ThumbnailFormatPNG,
		Pages:  []int{1, 2},
	})
	if err != nil {
		t.Fatalf("Thumbnails err = %v", err)
	}
	if len(thumbs) != 2 {
		t.Fatalf("len(thumbs) = %d, want 2", len(thumbs))
	}
	if thumbs[0].Page != 1 || thumbs[1].Data != "BBBB" {
		t.Errorf("thumbs = %+v, wrong page/data", thumbs)
	}
	// Wire body must be {"thumbnails":{...}} per the deployed-API quirk.
	body := string(bodySeen)
	if !strings.Contains(body, `"thumbnails":{`) {
		t.Errorf("body = %s, want top-level {\"thumbnails\":{...}} wrap", body)
	}
	if !strings.Contains(body, `"width":840`) || !strings.Contains(body, `"format":"png"`) || !strings.Contains(body, `"pages":[1,2]`) {
		t.Errorf("body = %s, missing expected option fields", body)
	}
}

func TestDocuments_Thumbnails_errorReturnsTypedError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":"VALIDATION_ERROR","message":"width is required"}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Documents.Thumbnails(context.Background(), "doc_abc", polipage.ThumbnailOptions{})
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != "VALIDATION_ERROR" || pErr.StatusCode != 400 {
		t.Fatalf("err = %v, want *Error{Code:VALIDATION_ERROR, StatusCode:400}", err)
	}
}

func TestDocuments_Delete_succeedsOn204(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	if err := client.Documents.Delete(context.Background(), "doc_abc"); err != nil {
		t.Fatalf("Delete err = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestDocuments_Delete_succeedsOn200WithBody(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"deleted":true}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	if err := client.Documents.Delete(context.Background(), "doc_abc"); err != nil {
		t.Fatalf("Delete err = %v", err)
	}
}

func TestDocuments_Delete_alreadyDeletedReturnsErrGone(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = io.WriteString(w, `{"code":"GONE","message":"already deleted"}`)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.Documents.Delete(context.Background(), "doc_abc")
	if !errors.Is(err, polipage.ErrGone) {
		t.Fatalf("err = %v, want errors.Is(..., ErrGone)", err)
	}
}

func TestDocuments_Delete_perCallWithHeaderAttachesHeader(t *testing.T) {
	t.Parallel()
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Trace-Id")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.Documents.Delete(
		context.Background(),
		"doc_abc",
		option.WithHeader("X-Trace-Id", "trace-del-1"),
	)
	if err != nil {
		t.Fatalf("Delete err = %v", err)
	}
	if got != "trace-del-1" {
		t.Errorf("X-Trace-Id = %q, want trace-del-1", got)
	}
}

func TestDocuments_Get_perCallWithHeaderAttachesHeader(t *testing.T) {
	t.Parallel()
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Trace-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, canonicalDescriptorJSON())
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Documents.Get(
		context.Background(),
		"doc_abc",
		option.WithHeader("X-Trace-Id", "trace-get-1"),
	)
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	if got != "trace-get-1" {
		t.Errorf("X-Trace-Id = %q, want trace-get-1", got)
	}
}

func TestDocuments_Preview_perCallWithHeaderAttachesHeader(t *testing.T) {
	t.Parallel()
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Trace-Id")
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<p>preview</p>")
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.Documents.Preview(
		context.Background(),
		"doc_abc",
		option.WithHeader("X-Trace-Id", "trace-prev-1"),
	)
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if got != "trace-prev-1" {
		t.Errorf("X-Trace-Id = %q, want trace-prev-1", got)
	}
}

func TestDocuments_Get_perCallWithRequestTimeoutOverridesClient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(1 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	client := newTestClient(t, server, option.WithMaxRetries(0))
	_, err := client.Documents.Get(
		context.Background(),
		"doc_abc",
		option.WithRequestTimeout(50*time.Millisecond),
	)
	if !errors.Is(err, polipage.ErrTimeout) {
		t.Fatalf("err = %v, want errors.Is(..., ErrTimeout)", err)
	}
}
