package polipage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// The presigned download is bounded by the client's timeout: as a whole for
// DownloadPDF and Render.PDF, as an idle timeout for Render.PDFStream. These
// tests use short real delays against a local server. Every delay that must
// NOT trip a timeout is a small fraction of it, and every server wait that
// must trip one lasts until the client goes away, so a slow runner can only
// make them slower, not wrong.

// hangPDF never answers: it waits until the client goes away.
func hangPDF(w http.ResponseWriter, r *http.Request) {
	<-r.Context().Done()
}

// tricklePDF sends the PDF in chunks bytes long, flushing each, with gap
// between two chunks. It stops when the client goes away.
func tricklePDF(pdf []byte, chunk int, gap time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		for i := 0; i < len(pdf); i += chunk {
			if i > 0 {
				select {
				case <-time.After(gap):
				case <-r.Context().Done():
					return
				}
			}
			_, _ = w.Write(pdf[i:min(i+chunk, len(pdf))])
			w.(http.Flusher).Flush()
		}
	}
}

func wantDownloadTimeout(t *testing.T, err error) {
	t.Helper()
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeDownloadFailed {
		t.Fatalf("err = %v (%T), want *Error with Code=DOWNLOAD_FAILED", err, err)
	}
	if pErr.Message != "Failed to download PDF: timeout" || pErr.StatusCode != 0 || pErr.RequestID != "" {
		t.Errorf("err = %+v, want message %q, no status and no request id", pErr, "Failed to download PDF: timeout")
	}
	if pErr.IsRetryable() || pErr.IsNetworkError() {
		t.Errorf("IsRetryable() = %v, IsNetworkError() = %v; want false, false", pErr.IsRetryable(), pErr.IsNetworkError())
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false for %v", err)
	}
	if got := pErr.ToPayload().Status; got != 0 {
		t.Errorf("ToPayload().Status = %d, want 0 (null)", got)
	}
}

// within fails when elapsed is far beyond the timeout that should have ended
// the call: the call did not wait for the server's hang.
func within(t *testing.T, start time.Time, limit time.Duration) {
	t.Helper()
	if elapsed := time.Since(start); elapsed > limit {
		t.Errorf("the call took %v, want it ended by the timeout (well under %v)", elapsed, limit)
	}
}

func TestRender_PDF_clientTimeoutBoundsTheDownload(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, hangPDF)
	client := newTestClient(t, server, option.WithTimeout(150*time.Millisecond))
	start := time.Now()
	_, err := client.Render.PDF(context.Background(), minimalInput())
	wantDownloadTimeout(t, err)
	within(t, start, 10*time.Second)
}

func TestRender_PDF_perCallTimeoutBoundsTheDownload(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, hangPDF)
	client := newTestClient(t, server)
	start := time.Now()
	_, err := client.Render.PDF(context.Background(), minimalInput(), option.WithRequestTimeout(150*time.Millisecond))
	wantDownloadTimeout(t, err)
	within(t, start, 10*time.Second)
}

func TestDownloadPDF_timeoutBoundsTheWholeDownload(t *testing.T) {
	t.Parallel()
	// Every gap (20 ms) is well under the timeout, but the whole body takes
	// 40 × 20 ms = 800 ms: the total is what DownloadPDF bounds.
	pdf := bytes.Repeat([]byte("x"), 40)
	server := downloadServer(t, tricklePDF(pdf, 1, 20*time.Millisecond))
	client := newTestClient(t, server, option.WithTimeout(200*time.Millisecond))
	doc, err := client.Render.Document(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	_, err = doc.DownloadPDF(context.Background())
	wantDownloadTimeout(t, err)
}

func TestDownloadPDF_downloadTimeoutOverridesTheClientTimeout(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, hangPDF)
	client := newTestClient(t, server, option.WithTimeout(time.Minute))
	doc, err := client.Render.Document(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	start := time.Now()
	_, err = doc.DownloadPDF(context.Background(), option.WithDownloadTimeout(150*time.Millisecond))
	wantDownloadTimeout(t, err)
	within(t, start, 10*time.Second)
}

func TestDownloadPDF_downloadWithinTheTimeoutSucceeds(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-1.4\nok\n%%EOF")
	server := downloadServer(t, tricklePDF(pdf, 4, time.Millisecond))
	client := newTestClient(t, server, option.WithTimeout(10*time.Second))
	got, err := client.Render.PDF(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("PDF err = %v", err)
	}
	if !bytes.Equal(got, pdf) {
		t.Errorf("PDF = %q, want %q", got, pdf)
	}
}

func TestRender_PDFStream_timeoutBeforeTheAnswer(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, hangPDF)
	client := newTestClient(t, server, option.WithTimeout(150*time.Millisecond))
	start := time.Now()
	stream, err := client.Render.PDFStream(context.Background(), minimalInput())
	if err == nil {
		_ = stream.Close()
	}
	wantDownloadTimeout(t, err)
	within(t, start, 10*time.Second)
}

func TestRender_PDFStream_idleTimeoutMidStream(t *testing.T) {
	t.Parallel()
	// 10 bytes, then nothing while the client is there.
	server := downloadServer(t, partialPDF(false))
	client := newTestClient(t, server, option.WithTimeout(200*time.Millisecond))
	stream, err := client.Render.PDFStream(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("PDFStream err = %v", err)
	}
	defer func() { _ = stream.Close() }()
	first := make([]byte, 10)
	if _, err := io.ReadFull(stream, first); err != nil {
		t.Fatalf("first bytes: %v", err)
	}
	start := time.Now()
	_, err = io.ReadAll(stream)
	wantDownloadTimeout(t, err)
	within(t, start, 4*time.Second)
}

func TestRender_PDFStream_idleTimeoutIsNotATotal(t *testing.T) {
	t.Parallel()
	// Each gap (20 ms) is a tenth of the timeout, while the whole body
	// takes 30 × 20 ms = 600 ms, three times the timeout.
	pdf := bytes.Repeat([]byte("y"), 30)
	server := downloadServer(t, tricklePDF(pdf, 1, 20*time.Millisecond))
	client := newTestClient(t, server, option.WithTimeout(200*time.Millisecond))
	stream, err := client.Render.PDFStream(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("PDFStream err = %v", err)
	}
	defer func() { _ = stream.Close() }()
	got, err := io.ReadAll(stream)
	if err != nil {
		t.Fatalf("ReadAll err = %v", err)
	}
	if !bytes.Equal(got, pdf) {
		t.Errorf("stream = %q, want %q", got, pdf)
	}
}

func TestRender_PDFStream_callerPaceDoesNotCount(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-1.4\nslow reader\n%%EOF")
	server := downloadServer(t, tricklePDF(pdf, 8, 0))
	client := newTestClient(t, server, option.WithTimeout(100*time.Millisecond))
	stream, err := client.Render.PDFStream(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("PDFStream err = %v", err)
	}
	defer func() { _ = stream.Close() }()
	// The caller waits three timeouts before each read: only the waits on
	// the storage count, so the stream must not fail.
	var got []byte
	buf := make([]byte, 8)
	for {
		time.Sleep(300 * time.Millisecond)
		n, err := stream.Read(buf)
		got = append(got, buf[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Read err = %v", err)
		}
	}
	if !bytes.Equal(got, pdf) {
		t.Errorf("stream = %q, want %q", got, pdf)
	}
}

func TestDownload_callerDeadlineIsNotLabelledTimeout(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, hangPDF)
	client := newTestClient(t, server)
	doc, err := client.Render.Document(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	// The caller's own deadline ends the download, not the SDK's timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err = doc.DownloadPDF(ctx)
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeDownloadFailed {
		t.Fatalf("err = %v, want Code=DOWNLOAD_FAILED", err)
	}
	if pErr.Message != "Failed to download PDF: network error" {
		t.Errorf("Message = %q, want the network error message: the SDK timeout did not fire", pErr.Message)
	}
}

func TestDownload_httpClientTimeoutIsNotLabelledTimeout(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, hangPDF)
	// The caller's own *http.Client gives up first; the SDK timeout is far.
	httpClient := server.Client()
	httpClient.Timeout = 150 * time.Millisecond
	client := newTestClient(t, server, option.WithHTTPClient(httpClient), option.WithTimeout(time.Minute))
	doc, err := client.Render.Document(context.Background(), minimalInput())
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	_, err = doc.DownloadPDF(context.Background())
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeDownloadFailed {
		t.Fatalf("err = %v, want Code=DOWNLOAD_FAILED", err)
	}
	if pErr.Message != "Failed to download PDF: network error" {
		t.Errorf("Message = %q, want the network error message: the SDK timeout did not fire", pErr.Message)
	}
}

func TestRender_PDFStream_cancelBeforeTheAnswerIsAbortedNotTimeout(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, hangPDF)
	client := newTestClient(t, server, option.WithTimeout(time.Minute))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	stream, err := client.Render.PDFStream(ctx, minimalInput())
	if err == nil {
		_ = stream.Close()
	}
	wantAborted(t, err)
}
