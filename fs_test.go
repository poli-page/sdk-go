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
	"testing"

	polipage "github.com/poli-page/sdk-go"
)

func TestRenderToFile_writesPDFBytes(t *testing.T) {
	t.Parallel()
	pdf := []byte("%PDF-1.4\nrender-to-file content\n%%EOF")
	server := twoHopServer(t, pdf, nil)
	client := newTestClient(t, server)

	dst := filepath.Join(t.TempDir(), "out.pdf")
	if err := polipage.RenderToFile(context.Background(), client, polipage.ProjectModeInput{
		Project: "billing", Template: "invoice",
		Version: polipage.Opt("1.0.0"),
		Data:    map[string]any{},
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
	if err := polipage.RenderToFile(context.Background(), client, polipage.ProjectModeInput{
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
	if err := polipage.RenderToFile(context.Background(), client, polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	}, dst); err != nil {
		t.Fatalf("RenderToFile err = %v", err)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, pdf) {
		t.Errorf("file = %q, want %q (existing content should be overwritten)", got, pdf)
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
	err := polipage.RenderToFile(context.Background(), client, polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	}, dst)
	if !errors.Is(err, polipage.ErrValidation) {
		t.Fatalf("err = %v, want errors.Is(..., ErrValidation)", err)
	}
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Errorf("file exists at %s; render error should leave no partial file", dst)
	}
}
