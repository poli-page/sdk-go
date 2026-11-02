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
	if want := "Failed to write PDF: " + pathErr.Error(); pErr.Code != "io_error" || pErr.Message != want {
		t.Errorf("err = %q / %q, want io_error / %q", pErr.Code, pErr.Message, want)
	}
	if pErr.StatusCode != 0 || pErr.IsRetryable() {
		t.Errorf("StatusCode = %d, IsRetryable = %v; want 0, false", pErr.StatusCode, pErr.IsRetryable())
	}
	if n := renders.Load(); n != 0 {
		t.Errorf("render calls = %d, want 0: a path that cannot be written costs no render", n)
	}
}

func TestRenderToFile_renameFailureIsIOErrorAndCleansUp(t *testing.T) {
	t.Parallel()
	server := twoHopServer(t, []byte("%PDF-1.4\nx"), nil)
	client := newTestClient(t, server)

	// The target is a non-empty directory: the temporary file is written,
	// but cannot be renamed over it.
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.pdf")
	if err := os.MkdirAll(filepath.Join(dst, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := polipage.RenderToFile(context.Background(), client, polipage.RenderInput{
		Project: "x", Template: "y", Data: map[string]any{},
	}, dst)
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != polipage.ErrCodeIOError {
		t.Fatalf("err = %v, want *Error with Code=io_error", err)
	}
	if !strings.HasPrefix(pErr.Message, "Failed to write PDF: ") || pErr.Cause == nil {
		t.Errorf("err = %+v, want the message prefix and a Cause", pErr)
	}
	if info, statErr := os.Stat(dst); statErr != nil || !info.IsDir() {
		t.Errorf("target changed: %v, %v", info, statErr)
	}
	wantOnlyEntries(t, dir, "out.pdf")
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
