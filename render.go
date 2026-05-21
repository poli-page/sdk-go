package polipage

import (
	"context"
	"encoding/json"
	"io"

	"github.com/poli-page/sdk-go/internal/clientconfig"
	"github.com/poli-page/sdk-go/internal/constants"
	"github.com/poli-page/sdk-go/internal/uuid"
	"github.com/poli-page/sdk-go/option"
)

// Render is the render namespace exposed as client.Render. Phase 2 ships
// Preview only; PDF, PDFStream, and Document land in Phase 3.
type Render struct {
	tr transport
}

// Preview generates paginated HTML from the input. Accepts both
// [ProjectModeInput] and [InlineModeInput] — that is the only render
// method that allows inline-mode HTML.
//
// Variadic options apply per-call. The most useful one is
// [option.WithIdempotencyKey] when the caller has a natural identifier
// (e.g. an invoice number) and wants to override the auto-generated
// UUID4.
func (r *Render) Preview(ctx context.Context, in RenderInput, opts ...option.RequestOption) (*PreviewResult, error) {
	var perCall clientconfig.Config
	for _, opt := range opts {
		if err := opt(&perCall); err != nil {
			return nil, &Error{Code: ErrCodeInvalidOptions, Message: err.Error(), Cause: err}
		}
	}
	idempotencyKey := perCall.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = uuid.New()
	}

	resp, err := r.tr.post(ctx, constants.PathRenderPreview, in, idempotencyKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result PreviewResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Drain any remaining bytes so the connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, &Error{
			Code:       ErrCodeInternalError,
			StatusCode: resp.StatusCode,
			Message:    "failed to decode response body: " + err.Error(),
			Cause:      err,
		}
	}
	return &result, nil
}
