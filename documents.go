package polipage

import (
	"context"
	"encoding/json"
	"io"
	"strconv"

	"github.com/poli-page/sdk-go/internal/constants"
	"github.com/poli-page/sdk-go/option"
)

// Documents is the stored-documents namespace exposed as client.Documents.
// It hosts the four lifecycle methods defined in spec §6.
type Documents struct {
	client *Client
}

// Get retrieves a stored document's descriptor with a fresh presigned URL.
// The returned descriptor's [DocumentDescriptor.DownloadPDF] uses the
// parent client for transport; calling it after the URL's ~15-minute TTL
// returns *Error{Code: ErrCodeDownloadFailed} — re-fetch via Get.
//
// Spec §6.1.
func (d *Documents) Get(ctx context.Context, id string) (*DocumentDescriptor, error) {
	resp, err := d.client.get(ctx, constants.PathDocument(id))
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
	doc.client = d.client
	return &doc, nil
}

// Preview retrieves a stored document's paginated HTML. The engine does
// no work — this is a cheap read.
//
// The deployed API returns the HTML as text/html (not a JSON envelope)
// and the page count via the X-Document-Page-Count response header.
// Missing or unparseable headers are tolerated: PageCount defaults to 0.
//
// Spec §6.2.
func (d *Documents) Preview(ctx context.Context, id string) (*DocumentPreviewResult, error) {
	resp, err := d.client.get(ctx, constants.PathDocumentPreview(id))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	html, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &Error{
			Code:       ErrCodeInternalError,
			StatusCode: resp.StatusCode,
			Message:    "failed to read response body: " + err.Error(),
			Cause:      err,
		}
	}
	pageCount := 0
	if raw := resp.Header.Get(constants.HeaderDocumentPageCount); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			pageCount = n
		}
	}
	return &DocumentPreviewResult{HTML: string(html), PageCount: pageCount}, nil
}

// Thumbnails generates page thumbnails for a stored document. Width is
// required. Pass [polipage.ThumbnailFormatJPEG] with a Quality value for
// JPEG output; otherwise PNG is returned.
//
// The deployed API expects the options wrapped under a top-level
// "thumbnails" key on the wire and returns the array under the same key —
// both are handled here so callers see a plain slice.
//
// Spec §6.3.
func (d *Documents) Thumbnails(ctx context.Context, id string, options ThumbnailOptions, opts ...option.RequestOption) ([]Thumbnail, error) {
	idempotencyKey, err := resolveIdempotencyKey(opts)
	if err != nil {
		return nil, err
	}
	wire := struct {
		Thumbnails ThumbnailOptions `json:"thumbnails"`
	}{Thumbnails: options}

	resp, err := d.client.post(ctx, constants.PathDocumentThumbnails(id), wire, idempotencyKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Thumbnails []Thumbnail `json:"thumbnails"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, &Error{
			Code:       ErrCodeInternalError,
			StatusCode: resp.StatusCode,
			Message:    "failed to decode response body: " + err.Error(),
			Cause:      err,
		}
	}
	return envelope.Thumbnails, nil
}

// Delete soft-deletes a stored document. The PDF is purged from storage;
// metadata is retained for audit. Already-deleted documents surface as
// [ErrGone] (HTTP 410) — use errors.Is to branch on it.
//
// Spec §6.4.
func (d *Documents) Delete(ctx context.Context, id string) error {
	return d.client.delete(ctx, constants.PathDocument(id))
}
