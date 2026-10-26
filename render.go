package polipage

import (
	"context"
	"encoding/json"
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
		return nil, &Error{Code: ErrCodeProjectRequiredForDocument, Message: "Render.Document/PDF/PDFStream require RenderInput.Project"}
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
		return nil, &Error{
			Code:       ErrCodeInternalError,
			StatusCode: resp.StatusCode,
			Message:    "failed to decode response body: " + err.Error(),
			Cause:      err,
		}
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
// retry. Returns *Error with Code == ErrCodeDownloadFailed on non-2xx or
// network failure.
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

// downloadStream performs a plain unauthenticated GET against a presigned
// URL and returns the response body for streaming. Shared by
// Render.PDFStream and DocumentDescriptor.DownloadPDF.
//
// The returned body uses the SDK's *http.Client (for TLS, proxy, etc.)
// but the request itself carries no SDK headers and is NOT subject to
// the retry loop. When timeout > 0 and the caller's ctx has no deadline,
// a per-call context.WithTimeout is applied; cancel ownership transfers to
// the returned body via cancelOnClose so the context is released when the
// caller closes the body.
func (c *Client) downloadStream(ctx context.Context, url string, timeout time.Duration) (io.ReadCloser, error) {
	if timeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			// cancel is transferred to the returned body's cancelOnClose so
			// it is released when the caller closes the body. If the HTTP
			// call errors out below, the deferred call cancels the context.
			defer func() {
				if cancel != nil {
					cancel()
				}
			}()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, &Error{Code: ErrCodeDownloadFailed, Message: err.Error(), Cause: err}
			}
			resp, err := c.cfg.HTTPClient.Do(req)
			if err != nil {
				return nil, &Error{Code: ErrCodeDownloadFailed, Message: err.Error(), Cause: err}
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				return nil, &Error{
					Code:       ErrCodeDownloadFailed,
					StatusCode: resp.StatusCode,
					Message:    fmt.Sprintf("failed to download PDF: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
				}
			}
			if resp.Body == nil {
				return nil, &Error{Code: ErrCodeInternalError, StatusCode: resp.StatusCode, Message: "presigned-URL response has no body"}
			}
			wrapped := &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
			cancel = nil // transfer ownership: deferred call above no-ops now.
			return wrapped, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &Error{Code: ErrCodeDownloadFailed, Message: err.Error(), Cause: err}
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, &Error{Code: ErrCodeDownloadFailed, Message: err.Error(), Cause: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return nil, &Error{
			Code:       ErrCodeDownloadFailed,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("failed to download PDF: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		}
	}
	if resp.Body == nil {
		return nil, &Error{Code: ErrCodeInternalError, StatusCode: resp.StatusCode, Message: "presigned-URL response has no body"}
	}
	return resp.Body, nil
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
