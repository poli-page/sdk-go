package polipage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// RenderToFile renders a PDF and streams the bytes to path. Parent
// directories are created if missing; an existing file at path is
// replaced. Memory-bounded — streaming via io.Copy regardless of PDF size.
//
// The PDF is written to a temporary file in path's directory, which is
// renamed over path only once the whole PDF is written. When anything fails
// (the render, the download or the write), path is left untouched: an
// existing file keeps its content and a missing one is not created. The
// directory and the temporary file are prepared before the render, so a
// path that cannot be written costs no render.
//
// Errors from the render call and the download propagate as-is (typed
// *Error, see [Render.PDFStream]). A file that cannot be written surfaces as
// *Error with Code == ErrCodeIOError, no status, the message
// "Failed to write PDF: <reason>" where <reason> is the os error's own text,
// and that error wrapped in Cause. It is never retryable.
//
// Callers should reuse a single *Client across calls. RenderToFile is a free
// function because it is a thin convenience over Render.PDFStream that makes
// no other network call.
func RenderToFile(ctx context.Context, c *Client, in RenderInput, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return writeError(err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return writeError(err)
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	body, err := c.Render.PDFStream(ctx, in)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	src := &readErrorReader{r: body}
	if _, err := io.Copy(tmp, src); err != nil {
		if src.err != nil {
			// The download failed: already an *Error (DOWNLOAD_FAILED, aborted).
			return src.err
		}
		return writeError(err)
	}
	// os.CreateTemp creates the file 0600; give the PDF the mode an existing
	// target has, else the usual 0644.
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	}
	if err := tmp.Chmod(mode); err != nil {
		return writeError(err)
	}
	if err := tmp.Close(); err != nil {
		return writeError(err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return writeError(err)
	}
	renamed = true
	return nil
}

// writeError is RenderToFile's error for a file it could not write.
func writeError(err error) *Error {
	return &Error{Code: ErrCodeIOError, Message: "Failed to write PDF: " + err.Error(), Cause: err}
}

// readErrorReader remembers the error its reader returned, so that an
// io.Copy failure can be told apart: a read error is the download's, any
// other is the file's.
type readErrorReader struct {
	r   io.Reader
	err error
}

func (r *readErrorReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		r.err = err
	}
	return n, err
}
