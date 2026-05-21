package polipage

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// RenderToFile renders a PDF and streams the bytes straight to disk.
// Parent directories are created if missing; an existing file at path
// is overwritten. Memory-bounded — streaming via io.Copy regardless of
// PDF size.
//
// Errors from the render call propagate as-is (typed *Error). I/O errors
// surface as *Error with Code == ErrCodeInvalidOptions and the underlying
// os error wrapped in Cause — use errors.Unwrap to inspect.
//
// Method order matches the spec: callers should reuse a single *Client
// across calls. RenderToFile is a free function because it is a thin
// convenience over Render.PDFStream; keeping it off *Client matches the
// platform spec §2.
func RenderToFile(ctx context.Context, c *Client, in ProjectModeInput, path string) error {
	body, err := c.Render.PDFStream(ctx, in)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return &Error{Code: ErrCodeInvalidOptions, Message: "failed to create directory: " + err.Error(), Cause: err}
	}

	f, err := os.Create(path)
	if err != nil {
		return &Error{Code: ErrCodeInvalidOptions, Message: "failed to create file: " + err.Error(), Cause: err}
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, body); err != nil {
		return &Error{Code: ErrCodeInvalidOptions, Message: "failed to write file: " + err.Error(), Cause: err}
	}
	return nil
}
