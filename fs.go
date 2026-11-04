package polipage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// errNotRegularFile is the reason RenderToFile refuses an existing target
// that is not a regular file.
var errNotRegularFile = errors.New("not a regular file")

// maxSymlinkHops bounds the symlinks RenderToFile follows to find the file it
// writes, so a link loop fails instead of spinning.
const maxSymlinkHops = 40

// RenderToFile renders a PDF and streams the bytes to path. Parent
// directories are created if missing; an existing file at path is
// replaced. Memory-bounded — streaming via io.Copy regardless of PDF size.
//
// When path is a symbolic link (or a chain of them), the file it points to
// is written and the link stays a link. The PDF is written to a temporary
// file next to that file, which is renamed over it only once the whole PDF
// is written. When anything fails (the render, the download or the write),
// the target is left untouched: an existing file keeps its content and a
// missing one is not created. The directory and the temporary file are
// prepared before the render, so a path that cannot be written costs no
// render.
//
// An existing target keeps its permission bits, without setuid, setgid or
// sticky. Because the PDF replaces it with a new file, its owner and group
// become those of the calling process, and other hard links to it keep the
// old content. An existing target that is not writable (a read-only file) or
// not a regular file (a directory, FIFO, socket or device such as
// /dev/stdout) is not replaced: RenderToFile fails before any API call, with
// the reason "not a regular file" for the latter. To write the PDF to a pipe
// or to stdout, copy [Render.PDFStream] into it instead.
//
// Errors from the render call and the download propagate as-is (typed
// *Error, see [Render.PDFStream]). A file that cannot be written surfaces as
// *Error with Code == ErrCodeIOError, no status, the message
// "Failed to write PDF: <path>: <reason>", where <reason> is the os error's
// own text (for example "Failed to write PDF: /out/invoice.pdf: permission
// denied"), and that error wrapped in Cause. It is never retryable.
//
// Callers should reuse a single *Client across calls. RenderToFile is a free
// function because it is a thin convenience over Render.PDFStream that makes
// no other network call.
func RenderToFile(ctx context.Context, c *Client, in RenderInput, path string) error {
	target, err := resolveTarget(path)
	if err != nil {
		return writeError(path, err)
	}
	// os.CreateTemp creates the file 0600; an existing target's mode is put
	// on it before the rename, a new target gets the usual 0644.
	mode := fs.FileMode(0o644)
	switch info, err := os.Stat(target); {
	case err == nil && !info.Mode().IsRegular():
		// A directory, FIFO, socket or device: judged by its attributes,
		// never opened, and never replaced by the rename.
		return writeError(path, &fs.PathError{Op: "open", Path: target, Err: errNotRegularFile})
	case err == nil:
		// Opened for writing, not truncated: a read-only target is refused
		// here, before the render, rather than replaced by the rename.
		f, err := os.OpenFile(target, os.O_WRONLY, 0)
		if err != nil {
			return writeError(path, err)
		}
		_ = f.Close()
		mode = info.Mode().Perm()
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return writeError(path, err)
	}

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return writeError(path, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return writeError(path, err)
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
		return writeError(path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return writeError(path, err)
	}
	if err := tmp.Close(); err != nil {
		return writeError(path, err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return writeError(path, err)
	}
	renamed = true
	return nil
}

// resolveTarget returns the file RenderToFile writes for path: path itself,
// or, when path is a symbolic link, the file at the end of its chain of
// links, which may not exist yet. A relative link is resolved against the
// real directory of the link, the way the OS follows it.
func resolveTarget(path string) (string, error) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real, nil
	}
	// The chain ends at a missing file (or path itself is missing): follow
	// it hop by hop.
	cur := path
	for range maxSymlinkHops {
		info, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return cur, nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			return cur, nil
		}
		link, err := os.Readlink(cur)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(realDir(filepath.Dir(cur)), link)
		}
		cur = link
	}
	return "", &fs.PathError{Op: "open", Path: path, Err: syscall.ELOOP}
}

// realDir is dir with its symbolic links resolved, or dir as it is when it
// cannot be resolved (it does not exist yet: RenderToFile creates it).
func realDir(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return dir
}

// writeError is RenderToFile's error for a file it could not write. The
// reason names the path the caller passed, never the temporary file, with
// the os error's own text; the os error itself is the Cause.
func writeError(path string, err error) *Error {
	reason := err.Error()
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	switch {
	case errors.As(err, &linkErr):
		reason = linkErr.Err.Error()
	case errors.As(err, &pathErr):
		reason = pathErr.Err.Error()
	}
	return &Error{Code: ErrCodeIOError, Message: fmt.Sprintf("Failed to write PDF: %s: %s", path, reason), Cause: err}
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
