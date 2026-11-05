package polipage_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	polipage "github.com/poli-page/sdk-go"
)

// downloadServer answers POST /v1/render with a descriptor whose presigned URL is /s3/doc.pdf,
// served by s3.
func downloadServer(t *testing.T, s3 http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/v1/render", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"documentId":"d","organizationId":"o","environment":"sandbox",
			"format":"A4","pageCount":1,"sizeBytes":1000,
			"createdAt":"2026-05-21T10:00:00Z","metadata":{},
			"presignedPdfUrl":"`+serverURL+`/s3/doc.pdf",
			"expiresAt":"2026-05-21T10:15:00Z"
		}`)
	})
	mux.HandleFunc("/s3/doc.pdf", s3)
	server := httptest.NewServer(mux)
	serverURL = server.URL
	t.Cleanup(server.Close)
	return server
}

// partialPDF announces 1000 bytes and sends 10, then waits until the client goes away, or
// returns at once when drop is set: the server then closes the connection mid-body.
func partialPDF(drop bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", "1000")
		_, _ = io.WriteString(w, "%PDF-1.4\n%")
		w.(http.Flusher).Flush()
		if drop {
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}
}

// arrivalCanceller is a storage that cancels the caller's ctx once the download request is
// in, then never answers: the cancel always lands while the download waits for the storage.
type arrivalCanceller struct {
	cancel context.CancelFunc
	calls  atomic.Int32
}

func cancelOnArrival(cancel context.CancelFunc) *arrivalCanceller {
	return &arrivalCanceller{cancel: cancel}
}

func (a *arrivalCanceller) handler(w http.ResponseWriter, r *http.Request) {
	a.calls.Add(1)
	a.cancel()
	<-r.Context().Done()
}

func (a *arrivalCanceller) wantOneCall(t *testing.T) {
	t.Helper()
	if n := a.calls.Load(); n != 1 {
		t.Errorf("storage calls = %d, want 1: the cancel did not land while the download waited", n)
	}
}

func wantAborted(t *testing.T, err error) {
	t.Helper()
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeAborted {
		t.Fatalf("err = %v (%T), want *Error with Code=aborted", err, err)
	}
	if pErr.Message != "Request was aborted" || pErr.StatusCode != 0 {
		t.Errorf("err = %+v, want message %q and no status", pErr, "Request was aborted")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false for %v", err)
	}
}

func wantDownloadNetworkError(t *testing.T, err error) {
	t.Helper()
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeDownloadFailed {
		t.Fatalf("err = %v (%T), want *Error with Code=DOWNLOAD_FAILED", err, err)
	}
	if pErr.Message != "Failed to download PDF: network error" || pErr.StatusCode != 0 {
		t.Errorf("err = %+v, want message %q and no status", pErr, "Failed to download PDF: network error")
	}
	if pErr.Cause == nil || errors.Unwrap(err) != pErr.Cause {
		t.Errorf("Cause = %v, want the read error, reachable through Unwrap", pErr.Cause)
	}
	if pErr.IsRetryable() {
		t.Error("IsRetryable() = true, want false for a download network error")
	}
}

func TestDownloadPDF_cancelWhileReadingBodyIsAborted(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, partialPDF(false))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancel once the SDK has read the PDF's first 10 bytes: the cancel always lands mid-body.
	storage := &phaseTransport{streamPath: "/s3/doc.pdf", prefetch: 10, onRest: cancel}
	client := newTestClient(t, server, phaseClient(server, storage))
	doc, err := client.Render.Document(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	_, err = doc.DownloadPDF(ctx)
	wantAborted(t, err)
	if n := storage.read.Load(); n != 10 {
		t.Errorf("the SDK read %d PDF bytes, want the 10 the storage sent: the cancel did not land mid-body", n)
	}
}

func TestDownloadPDF_connectionDroppedMidBodyIsDownloadFailed(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, partialPDF(true))
	client := newTestClient(t, server)
	doc, err := client.Render.Document(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	_, err = doc.DownloadPDF(context.Background())
	wantDownloadNetworkError(t, err)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("errors.Is(err, io.ErrUnexpectedEOF) = false for %v", err)
	}
}

func TestRender_PDF_cancelBeforeStorageAnswersIsAborted(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	storage := cancelOnArrival(cancel)
	server := downloadServer(t, storage.handler)
	client := newTestClient(t, server)
	_, err := client.Render.PDF(ctx, minimalInput())
	wantAborted(t, err)
	storage.wantOneCall(t)
}

func TestRender_PDFStream_cancelWhileReadingIsAborted(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, partialPDF(false))
	client := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.Render.PDFStream(ctx, minimalInput())
	if err != nil {
		t.Fatalf("PDFStream err = %v", err)
	}
	defer func() { _ = stream.Close() }()
	first := make([]byte, 10)
	if _, err := io.ReadFull(stream, first); err != nil {
		t.Fatalf("first bytes: %v", err)
	}
	cancel()
	_, err = io.ReadAll(stream)
	wantAborted(t, err)
}

func TestRender_PDFStream_connectionDroppedMidBodyIsDownloadFailed(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, partialPDF(true))
	client := newTestClient(t, server)
	stream, err := client.Render.PDFStream(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("PDFStream err = %v", err)
	}
	defer func() { _ = stream.Close() }()
	_, err = io.ReadAll(stream)
	wantDownloadNetworkError(t, err)
}

func TestRender_PDF_storageStatusMessage(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	client := newTestClient(t, server)
	_, err := client.Render.PDF(context.Background(), minimalInput())
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeDownloadFailed {
		t.Fatalf("err = %v, want Code=DOWNLOAD_FAILED", err)
	}
	if pErr.Message != "Failed to download PDF: HTTP 503" || pErr.StatusCode != 503 {
		t.Errorf("err = %+v, want message %q and status 503", pErr, "Failed to download PDF: HTTP 503")
	}
}
