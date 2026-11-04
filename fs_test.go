package polipage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	polipage "github.com/poli-page/sdk-go"
)

func TestRenderToFile_writesPDFBytes(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-1.4\nrender-to-file content\n%%EOF")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)

	dst := filepath.Join(t.TempDir(), "out.pdf")
	if err := polipage.RenderToFile(context.Background(), client, polipage.RenderInput{
		Project: "billing", Template: "invoice",
		Data: map[string]any{},
	}, dst); err != nil {
		t.Fatalf("RenderToFile err = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read back err = %v", err)
	}
	if !bytes.Equal(got, pdf) {
		t.Fatalf("file bytes mismatch:\n got %q\nwant %q", got, pdf)
	}
}

func TestRenderToFile_createsParentDirectories(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-deep")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)

	dst := filepath.Join(t.TempDir(), "deeply", "nested", "out.pdf")
	if err := polipage.RenderToFile(context.Background(), client, polipage.RenderInput{
		Project: "x", Template: "y", Data: map[string]any{},
	}, dst); err != nil {
		t.Fatalf("RenderToFile err = %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("output file missing: %v", err)
	}
}

func TestRenderToFile_overwritesExistingFile(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-overwrite")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)

	dst := filepath.Join(t.TempDir(), "existing.pdf")
	if err := os.WriteFile(dst, []byte("stale content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := polipage.RenderToFile(context.Background(), client, polipage.RenderInput{
		Project: "x", Template: "y", Data: map[string]any{},
	}, dst); err != nil {
		t.Fatalf("RenderToFile err = %v", err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, pdf) {
		t.Errorf("file = %q, want %q (existing content should be overwritten)", got, pdf)
	}
}

func TestRenderToFile_unwritableDirectoryIsIOErrorWithoutRendering(t *testing.T) {
	t.Parallel()
	var renders atomic.Int32
	server := twoHopServer(t, []byte("%PDF-1.4\nx"), func([]byte) { renders.Add(1) })
	client := newTestClient(t, server)

	// Put a regular file where RenderToFile's MkdirAll wants a directory.
	// MkdirAll then fails because the parent path resolves to a file.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(blocker, "out.pdf")

	err := polipage.RenderToFile(context.Background(), client, polipage.RenderInput{
		Project: "x", Template: "y", Data: map[string]any{},
	}, dst)
	if !errors.Is(err, polipage.ErrIOError) {
		t.Fatalf("err = %v, want errors.Is(..., ErrIOError)", err)
	}
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		t.Fatalf("err = %v, want *polipage.Error", err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("Cause = %v, want the wrapped *os.PathError", pErr.Cause)
	}
	if want := "Failed to write PDF: " + dst + ": " + pathErr.Err.Error(); pErr.Code != "io_error" || pErr.Message != want {
		t.Errorf("err = %q / %q, want io_error / %q", pErr.Code, pErr.Message, want)
	}
	if pErr.StatusCode != 0 || pErr.IsRetryable() {
		t.Errorf("StatusCode = %d, IsRetryable = %v; want 0, false", pErr.StatusCode, pErr.IsRetryable())
	}
	if n := renders.Load(); n != 0 {
		t.Errorf("render calls = %d, want 0: a path that cannot be written costs no render", n)
	}
}

func TestRenderToFile_directoryTargetIsIOErrorWithoutRendering(t *testing.T) {
	t.Parallel()
	var renders atomic.Int32
	server := twoHopServer(t, []byte("%PDF-1.4\nx"), func([]byte) { renders.Add(1) })
	client := newTestClient(t, server)

	// The target is a non-empty directory: it cannot be replaced.
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.pdf")
	if err := os.MkdirAll(filepath.Join(dst, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := renderToFile(client, dst)
	pErr := wantIOErrorNaming(t, err, dst)
	if want := "Failed to write PDF: " + dst + ": not a regular file"; pErr.Message != want {
		t.Errorf("Message = %q, want %q", pErr.Message, want)
	}
	if info, statErr := os.Stat(dst); statErr != nil || !info.IsDir() {
		t.Errorf("target changed: %v, %v", info, statErr)
	}
	if n := renders.Load(); n != 0 {
		t.Errorf("render calls = %d, want 0", n)
	}
	wantOnlyEntries(t, dir, "out.pdf")
}

func TestRenderToFile_readOnlyTargetIsIOErrorWithoutRendering(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		t.Skip("root can write a read-only file")
	}
	var renders atomic.Int32
	server := twoHopServer(t, []byte("%PDF-1.4\nnew"), func([]byte) { renders.Add(1) })
	client := newTestClient(t, server)

	dir := t.TempDir()
	dst := filepath.Join(dir, "invoice.pdf")
	if err := os.WriteFile(dst, []byte("previous content"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dst, 0o600) }) // let TempDir remove it on Windows

	err := renderToFile(client, dst)
	pErr := wantIOErrorNaming(t, err, dst)
	if runtime.GOOS != "windows" {
		if want := "Failed to write PDF: " + dst + ": permission denied"; pErr.Message != want {
			t.Errorf("Message = %q, want %q", pErr.Message, want)
		}
	}
	if n := renders.Load(); n != 0 {
		t.Errorf("render calls = %d, want 0: the read-only target is refused before any API call", n)
	}
	got, readErr := os.ReadFile(dst)
	if readErr != nil || string(got) != "previous content" {
		t.Errorf("file = %q, %v; want the previous content untouched", got, readErr)
	}
	wantOnlyEntries(t, dir, "invoice.pdf")
}

func TestRenderToFile_existingTargetKeepsItsMode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX permission bits")
	}
	pdf := []byte("%PDF-1.4\nmode")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)

	dst := filepath.Join(t.TempDir(), "out.pdf")
	if err := os.WriteFile(dst, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Neither os.CreateTemp's 0600 nor the 0644 of a new target.
	if err := os.Chmod(dst, 0o640); err != nil { //nolint:gosec // G302: the mode under test
		t.Fatal(err)
	}
	if err := renderToFile(client, dst); err != nil {
		t.Fatalf("RenderToFile err = %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("mode = %o, want 0640 kept", got)
	}
}

// symlinkChain builds root/real/dir, root/sym -> real/dir and
// root/real/dir/link -> ../a.pdf. Through sym/link, the file is real/a.pdf:
// the relative link resolves against its real directory, not against sym.
func symlinkChain(t *testing.T) (root, linkPath, realFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "real", "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "dir"), filepath.Join(root, "sym")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "a.pdf"), filepath.Join(root, "real", "dir", "link")); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, "sym", "link"), filepath.Join(root, "real", "a.pdf")
}

func wantStillALink(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is no longer a symbolic link: %v, %v", path, info, err)
	}
}

func TestRenderToFile_symlinkChainUpdatesTheRealFile(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-1.4\nthrough the links")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)
	root, linkPath, realFile := symlinkChain(t)
	if err := os.WriteFile(realFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := renderToFile(client, linkPath); err != nil {
		t.Fatalf("RenderToFile err = %v", err)
	}
	got, err := os.ReadFile(realFile)
	if err != nil || !bytes.Equal(got, pdf) {
		t.Errorf("real/a.pdf = %q, %v; want the PDF", got, err)
	}
	wantStillALink(t, filepath.Join(root, "real", "dir", "link"))
	wantStillALink(t, filepath.Join(root, "sym"))
	wantOnlyEntries(t, filepath.Join(root, "real"), "a.pdf", "dir")
}

func TestRenderToFile_danglingSymlinkCreatesTheFileItNames(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-1.4\nnew through the links")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)
	root, linkPath, realFile := symlinkChain(t)

	if err := renderToFile(client, linkPath); err != nil {
		t.Fatalf("RenderToFile err = %v", err)
	}
	got, err := os.ReadFile(realFile)
	if err != nil || !bytes.Equal(got, pdf) {
		t.Errorf("real/a.pdf = %q, %v; want the PDF", got, err)
	}
	wantStillALink(t, filepath.Join(root, "real", "dir", "link"))
	wantOnlyEntries(t, root, "real", "sym")
}

func renderToFile(client *polipage.Client, path string) error {
	return polipage.RenderToFile(context.Background(), client, polipage.RenderInput{
		Project: "x", Template: "y", Data: map[string]any{},
	}, path)
}

// wantIOErrorNaming checks an io_error whose reason names path, never the
// temporary file.
func wantIOErrorNaming(t *testing.T, err error, path string) *polipage.Error {
	t.Helper()
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeIOError {
		t.Fatalf("err = %v, want *Error with Code=io_error", err)
	}
	if !strings.HasPrefix(pErr.Message, "Failed to write PDF: "+path+": ") || strings.Contains(pErr.Message, ".tmp") {
		t.Errorf("Message = %q, want it to name %s and not the temporary file", pErr.Message, path)
	}
	if pErr.Cause == nil || pErr.StatusCode != 0 || pErr.IsRetryable() {
		t.Errorf("err = %+v, want a Cause, no status, not retryable", pErr)
	}
	return pErr
}

func TestRenderToFile_downloadFailureLeavesExistingFileUntouched(t *testing.T) {
	t.Parallel()
	// The storage announces 1000 bytes, sends 10 and closes the connection.
	server := downloadServer(t, partialPDF(true))
	client := newTestClient(t, server)

	dir := t.TempDir()
	dst := filepath.Join(dir, "existing.pdf")
	if err := os.WriteFile(dst, []byte("previous content"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := polipage.RenderToFile(context.Background(), client, polipage.RenderInput{
		Project: "x", Template: "y", Data: map[string]any{},
	}, dst)
	wantDownloadNetworkError(t, err)
	got, readErr := os.ReadFile(dst)
	if readErr != nil || string(got) != "previous content" {
		t.Errorf("file = %q, %v; want the previous content untouched", got, readErr)
	}
	wantOnlyEntries(t, dir, "existing.pdf")
}

func TestRenderToFile_downloadFailureCreatesNoFile(t *testing.T) {
	t.Parallel()
	server := downloadServer(t, partialPDF(true))
	client := newTestClient(t, server)

	dir := t.TempDir()
	dst := filepath.Join(dir, "missing.pdf")
	err := polipage.RenderToFile(context.Background(), client, polipage.RenderInput{
		Project: "x", Template: "y", Data: map[string]any{},
	}, dst)
	wantDownloadNetworkError(t, err)
	wantOnlyEntries(t, dir)
}

// wantOnlyEntries fails unless dir holds exactly the names given: no
// temporary file is left behind.
func wantOnlyEntries(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !slices.Equal(got, names) {
		t.Errorf("entries of %s = %v, want %v", dir, got, names)
	}
}

func TestRenderToFile_renderErrorPropagatesWithoutCreatingFile(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":"VALIDATION_ERROR","message":"data is required"}`)
	}))
	defer server.Close()
	client := newTestClient(t, server)

	dst := filepath.Join(t.TempDir(), "should_not_exist.pdf")
	err := polipage.RenderToFile(context.Background(), client, polipage.RenderInput{
		Project: "x", Template: "y", Data: map[string]any{},
	}, dst)
	if !errors.Is(err, polipage.ErrValidation) {
		t.Fatalf("err = %v, want errors.Is(..., ErrValidation)", err)
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Errorf("file exists at %s; render error should leave no partial file", dst)
	}
}
