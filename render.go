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
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, bodyReadError(resp.StatusCode, "failed to decode response body: ", err)
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
func (r *Render) PDF(ctx context.Context, in RenderInput, opts ...option.RequestOption) ([]byte, error) {
	doc, err := r.Document(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return doc.DownloadPDF(ctx)
}

// PDFStream is like [Render.PDF] but returns the PDF as an [io.ReadCloser]
// so the caller can stream it directly to an HTTP response, an S3 upload,
// or a file without buffering.
//
// The caller MUST close the returned ReadCloser when done, even on early
// return paths.
func (r *Render) PDFStream(ctx context.Context, in RenderInput, opts ...option.RequestOption) (io.ReadCloser, error) {
	doc, err := r.Document(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	return r.client.downloadStream(ctx, doc.PresignedPDFURL, 0)
}

// DownloadPDF fetches the PDF bytes from PresignedPDFURL. The URL has a
// ~15-minute TTL — when it expires, call Documents.Get(id) to refresh and
// retry.
//
// A non-2xx answer returns *Error with Code == ErrCodeDownloadFailed and the
// storage status; a network failure, before the answer or while the body is
// read, returns ErrCodeDownloadFailed without a status. Canceling ctx returns
// ErrCodeAborted. The underlying error is wrapped in Cause.
//
// The request is unauthenticated and is not subject to the SDK's retry
// policy: presigned-URL fetches go directly to S3 (or equivalent).
func (d *DocumentDescriptor) DownloadPDF(ctx context.Context, opts ...option.DownloadOption) ([]byte, error) {
	if d.client == nil {
		return nil, &Error{Code: ErrCodeInvalidOptions, Message: "DocumentDescriptor has no client back-reference"}
	}
	var dl option.DownloadConfig
	for _, opt := range opts {
		opt(&dl)
	}
	body, err := d.client.downloadStream(ctx, d.PresignedPDFURL, dl.Timeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return io.ReadAll(body)
}

// Download error messages: the SDK specification's exact texts.
const (
	downloadStatusMessage  = "Failed to download PDF: HTTP %d"
	downloadNetworkMessage = "Failed to download PDF: network error"
)

// downloadStream performs a plain unauthenticated GET against a presigned
// URL and returns the response body for streaming. Shared by
// Render.PDFStream and DocumentDescriptor.DownloadPDF.
//
// The returned body uses the SDK's *http.Client (for TLS, proxy, etc.)
// but the request itself carries no SDK headers and is NOT subject to
// the retry loop. When timeout > 0 and the caller's ctx has no deadline,
// a per-call context.WithTimeout is applied; the returned body releases it
// on Close. A read error of the returned body is an *Error too (see
// downloadFailure).
func (c *Client) downloadStream(ctx context.Context, url string, timeout time.Duration) (io.ReadCloser, error) {
	cancel := context.CancelFunc(func() {})
	if timeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			ctx, cancel = context.WithTimeout(ctx, timeout)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, &Error{Code: ErrCodeDownloadFailed, Message: downloadNetworkMessage, Cause: err}
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		cancel()
		return nil, downloadFailure(ctx, err)
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
	return &downloadBody{body: resp.Body, ctx: ctx, cancel: cancel}, nil
}

// downloadBody is a presigned download's body whose read errors are SDK
// errors, so a cancellation or a dropped connection mid-body never escapes
// as a raw net/http error. Close releases the download's context.
type downloadBody struct {
	body   io.ReadCloser
	ctx    context.Context
	cancel context.CancelFunc
}

func (b *downloadBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, downloadFailure(b.ctx, err)
	}
	return n, err
}

func (b *downloadBody) Close() error {
	err := b.body.Close()
	b.cancel()
	return err
}

// downloadFailure is the error of a download that failed without an HTTP
// status, before the answer or while its body is read: ErrCodeAborted when
// ctx was canceled, else ErrCodeDownloadFailed without a status. Cause
// wraps err, and context.Canceled for an abort.
func downloadFailure(ctx context.Context, err error) *Error {
	if errors.Is(ctx.Err(), context.Canceled) {
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
