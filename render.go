package polipage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/poli-page/sdk-go/internal/clientconfig"
	"github.com/poli-page/sdk-go/internal/constants"
	"github.com/poli-page/sdk-go/internal/uuid"
	"github.com/poli-page/sdk-go/option"
)

// Render is the render namespace exposed as client.Render: PDF,
// PDFStream and Document, the spec §5 surface.
type Render struct {
	client *Client
}

// Document renders a PDF, stores it server-side, and returns the
// descriptor. The PDF bytes are NOT fetched — call (*DocumentDescriptor).DownloadPDF
// when you need them, or use [Render.PDF] / [Render.PDFStream] for the
// one-call shortcut.
//
// The API renders the project's copy published in the API key's
// environment. Variadic options apply per-call; the most useful one is
// [option.WithIdempotencyKey] when the caller has a natural identifier
// (e.g. an invoice number) and wants to override the auto-generated UUID4.
func (r *Render) Document(ctx context.Context, in RenderInput, opts ...option.RequestOption) (*DocumentDescriptor, error) {
	if in.Project == "" {
		return nil, &Error{Code: ErrCodeProjectRequiredForDocument, Message: "project is required"}
	}
	if err := validateMetadata(in.Metadata); err != nil {
		return nil, err
	}
	per, err := resolvePerCall(opts)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.post(ctx, constants.PathRender, in, per)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var doc DocumentDescriptor
	if err := r.client.decodeJSON(resp, &doc); err != nil {
		return nil, err
	}
	if doc.Metadata == nil {
		doc.Metadata = make(RenderMetadata)
	}
	doc.client = r.client
	return &doc, nil
}

// PDF renders a PDF and returns the raw bytes. Two HTTP calls under the
// hood: POST /v1/render to produce a stored document, then GET
// PresignedPDFURL to fetch the bytes.
//
// The download is bounded as a whole (body included) by the call's
// [option.WithRequestTimeout], else the client's [option.WithTimeout], on top
// of ctx: the first bound to end wins. Past the SDK's timeout, the call returns
// ErrCodeDownloadFailed with the message "Failed to download PDF: timeout" and
// no status.
func (r *Render) PDF(ctx context.Context, in RenderInput, opts ...option.RequestOption) ([]byte, error) {
	doc, err := r.Document(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return doc.DownloadPDF(ctx, option.WithDownloadTimeout(r.client.downloadTimeout(opts)))
}

// PDFStream is like [Render.PDF] but returns the PDF as an [io.ReadCloser]
// so the caller can stream it directly to an HTTP response, an S3 upload,
// or a file without buffering.
//
// The download's timeout (the call's [option.WithRequestTimeout], else the
// client's [option.WithTimeout]) is an idle timeout, on top of ctx (the first
// bound to end wins): it bounds the wait for the storage's answer and for
// each read of the stream, not the total, so a large PDF read at the caller's
// pace is not cut off. Past it, PDFStream or the stream's Read returns
// ErrCodeDownloadFailed with the message "Failed to download PDF: timeout".
//
// The caller MUST close the returned ReadCloser when done, even on early
// return paths.
func (r *Render) PDFStream(ctx context.Context, in RenderInput, opts ...option.RequestOption) (io.ReadCloser, error) {
	doc, err := r.Document(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return r.client.downloadStream(ctx, doc.PresignedPDFURL, r.client.downloadTimeout(opts), true)
}

// DownloadPDF fetches the PDF bytes from PresignedPDFURL. The URL has a
// ~15-minute TTL — when it expires, call Documents.Get(id) to refresh and
// retry.
//
// The whole download, body included, is bounded by [option.WithDownloadTimeout],
// else the client's [option.WithTimeout], on top of ctx: the first bound to
// end wins.
// A non-2xx answer returns *Error with Code == ErrCodeDownloadFailed and the
// storage status; a network failure, before the answer or while the body is
// read (a body that ends short of its Content-Length included), returns
// ErrCodeDownloadFailed without a status, and so does a download that outlasts
// the timeout, with the message "Failed to download PDF: timeout". Canceling
// ctx returns ErrCodeAborted. The underlying error is wrapped in Cause.
//
// The request is unauthenticated and is not subject to the SDK's retry
// policy: presigned-URL fetches go directly to S3 (or equivalent). A
// DOWNLOAD_FAILED is never retryable.
func (d *DocumentDescriptor) DownloadPDF(ctx context.Context, opts ...option.DownloadOption) ([]byte, error) {
	if d.client == nil {
		return nil, &Error{Code: ErrCodeInvalidOptions, Message: "DocumentDescriptor has no client back-reference"}
	}
	dl := option.DownloadConfig{Timeout: d.client.cfg.Timeout}
	for _, opt := range opts {
		opt(&dl)
	}
	body, err := d.client.downloadStream(ctx, d.PresignedPDFURL, dl.Timeout, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return io.ReadAll(body)
}

// downloadTimeout is the presigned download's timeout for a render call: the
// call's own timeout when it sets one, else the client's. It is the same
// value that bounds each API attempt.
func (c *Client) downloadTimeout(opts []option.RequestOption) time.Duration {
	var per clientconfig.Config
	for _, opt := range opts {
		// A failing option has already failed the render call.
		_ = opt(&per)
	}
	if per.Timeout > 0 {
		return per.Timeout
	}
	return c.cfg.Timeout
}

// Download error messages: the SDK specification's exact texts.
const (
	downloadStatusMessage  = "Failed to download PDF: HTTP %d"
	downloadNetworkMessage = "Failed to download PDF: network error"
	downloadTimeoutMessage = "Failed to download PDF: timeout"
)

// errDownloadTimeout is the cause the download's own timer cancels its
// context with. It is how a failure is known to be the timeout's doing,
// rather than the caller's cancellation or a dropped connection; it wraps
// context.DeadlineExceeded for errors.Is.
var errDownloadTimeout = fmt.Errorf("polipage: download timeout: %w", context.DeadlineExceeded)

// downloadStream performs a plain unauthenticated GET against a presigned
// URL and returns the response body for streaming. Shared by
// Render.PDFStream and DocumentDescriptor.DownloadPDF.
//
// The returned body uses the SDK's *http.Client (for TLS, proxy, etc.)
// but the request itself carries no SDK headers and is NOT subject to
// the retry loop. When timeout > 0, the download is bounded, on top of the
// caller's ctx (the first bound to end wins): as a whole (until Close) when
// idle is false; when
// idle is true, only while the SDK waits on the storage, for its answer or
// inside a Read, so the time the caller spends between two Reads does not
// count. A zero or negative timeout leaves the download unbounded, as
// before. A read error of the returned body is an *Error too (see
// downloadFailure).
func (c *Client) downloadStream(ctx context.Context, url string, timeout time.Duration, idle bool) (io.ReadCloser, error) {
	parent := ctx
	cancel := context.CancelFunc(func() {})
	var timer *time.Timer
	if timeout > 0 {
		// Derived from the caller's ctx, so its own deadline and cancellation
		// still apply: whichever bound comes first ends the download.
		if idle {
			var cancelCause context.CancelCauseFunc
			ctx, cancelCause = context.WithCancelCause(ctx)
			timer = time.AfterFunc(timeout, func() { cancelCause(errDownloadTimeout) })
			cancel = func() {
				timer.Stop()
				cancelCause(context.Canceled)
			}
		} else {
			ctx, cancel = context.WithTimeoutCause(ctx, timeout, errDownloadTimeout)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, &Error{Code: ErrCodeDownloadFailed, Message: downloadNetworkMessage, Cause: err}
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if timer != nil {
		timer.Stop()
	}
	if err != nil {
		failure := downloadFailure(parent, ctx, err)
		cancel()
		return nil, failure
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		cancel()
		return nil, &Error{
			Code:       ErrCodeDownloadFailed,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf(downloadStatusMessage, resp.StatusCode),
		}
	}
	return &downloadBody{body: resp.Body, parent: parent, ctx: ctx, cancel: cancel, idle: timer, timeout: timeout}, nil
}

// downloadBody is a presigned download's body whose read errors are SDK
// errors, so a cancellation, a timeout or a dropped connection mid-body
// never escapes as a raw net/http error. With an idle timer, each Read arms
// it for the read's duration only. Close releases the download's context.
type downloadBody struct {
	body    io.ReadCloser
	parent  context.Context
	ctx     context.Context
	cancel  context.CancelFunc
	idle    *time.Timer
	timeout time.Duration
}

func (b *downloadBody) Read(p []byte) (int, error) {
	if b.idle != nil {
		b.idle.Reset(b.timeout)
	}
	n, err := b.body.Read(p)
	if b.idle != nil {
		b.idle.Stop()
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, downloadFailure(b.parent, b.ctx, err)
	}
	return n, err
}

func (b *downloadBody) Close() error {
	err := b.body.Close()
	b.cancel()
	return err
}

// downloadFailure is the error of a download that failed without an HTTP
// status, before the answer or while its body is read. parent is the
// caller's context and ctx the download's own, derived from it.
//
//   - The download's own timer fired: ErrCodeDownloadFailed, "Failed to
//     download PDF: timeout", no status. Only the timer's cause on ctx says
//     so, never the elapsed time.
//   - The caller canceled parent: ErrCodeAborted.
//   - Anything else (a refused or dropped connection, a body short of its
//     Content-Length, the caller's own deadline): ErrCodeDownloadFailed,
//     "Failed to download PDF: network error", no status.
//
// Cause wraps err, and errDownloadTimeout or context.Canceled for the first
// two.
func downloadFailure(parent, ctx context.Context, err error) *Error {
	if errors.Is(context.Cause(ctx), errDownloadTimeout) {
		cause := err
		if !errors.Is(err, errDownloadTimeout) {
			cause = fmt.Errorf("%w: %w", errDownloadTimeout, err)
		}
		return &Error{Code: ErrCodeDownloadFailed, Message: downloadTimeoutMessage, Cause: cause}
	}
	if errors.Is(parent.Err(), context.Canceled) {
		cause := err
		if !errors.Is(err, context.Canceled) {
			cause = fmt.Errorf("%w: %w", context.Canceled, err)
		}
		return &Error{Code: ErrCodeAborted, Message: "Request was aborted", Cause: cause}
	}
	return &Error{Code: ErrCodeDownloadFailed, Message: downloadNetworkMessage, Cause: err}
}

// resolvePerCall folds the variadic per-call options into a Config and
// fills in defaults the transport relies on (auto UUID4 for Idempotency-Key
// when the caller didn't supply one).
func resolvePerCall(opts []option.RequestOption) (clientconfig.Config, error) {
	var per clientconfig.Config
	for _, opt := range opts {
		if err := opt(&per); err != nil {
			return per, &Error{Code: ErrCodeInvalidOptions, Message: err.Error(), Cause: err}
		}
	}
	if per.IdempotencyKey == "" {
		per.IdempotencyKey = uuid.New()
	}
	return per, nil
}

// validateMetadata checks that every value in m is a primitive
// (string / number / bool / nil / json.Number). Nested maps and slices are
// rejected up-front with ErrCodeInvalidOptions so the caller fails fast
// instead of seeing a server VALIDATION_ERROR after a round-trip.
func validateMetadata(m RenderMetadata) *Error {
	for k, v := range m {
		switch v.(type) {
		case nil,
			string, bool,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64,
			json.Number:
			// OK
		default:
			return &Error{
				Code:    ErrCodeInvalidOptions,
				Message: fmt.Sprintf("metadata value for key %q must be a primitive (string/number/bool); got %T", k, v),
			}
		}
	}
	return nil
}
