//go:build integration

package polipage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// Integration tests run against a live API. Gated by POLI_PAGE_API_KEY.
// Run with: go test -tags=integration ./...
//
// Defaults mirror the Node integration tests (sdk-node/tests/integration):
//   - POLI_PAGE_BASE_URL defaults to https://api-develop.poli.page
//   - The getting-started/welcome/1.0.0 template is provisioned for every
//     org so it works out of the box for any pp_test_* key.

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func integrationClient(t *testing.T) *polipage.Client {
	t.Helper()
	apiKey := os.Getenv("POLI_PAGE_API_KEY")
	if apiKey == "" {
		t.Skip("POLI_PAGE_API_KEY not set — skipping integration test")
	}
	return polipage.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(envOr("POLI_PAGE_BASE_URL", "https://api-develop.poli.page")),
	)
}

func TestIntegration_Render_Preview_inlineMode(t *testing.T) {
	client := integrationClient(t)
	res, err := client.Render.Preview(context.Background(), polipage.InlineModeInput{
		Template: "<p>{{ name }}</p>",
		Data:     map[string]any{"name": "Preview Test"},
	})
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if res.HTML == "" {
		t.Error("Preview returned empty HTML")
	}
	// Deployed API returns totalPages: 0 for tiny inline content (no page
	// breaks). Just confirm it is non-negative — mirror Node's tolerance.
	if res.TotalPages < 0 {
		t.Errorf("TotalPages = %d, want >= 0", res.TotalPages)
	}
	if res.Environment != polipage.EnvironmentSandbox && res.Environment != polipage.EnvironmentLive {
		t.Errorf("Environment = %q, want sandbox|live", res.Environment)
	}
}

func TestIntegration_Render_Preview_projectMode(t *testing.T) {
	client := integrationClient(t)
	res, err := client.Render.Preview(context.Background(), polipage.ProjectModeInput{
		Project:  envOr("POLI_PAGE_TEST_PROJECT", "getting-started"),
		Template: envOr("POLI_PAGE_TEST_TEMPLATE", "welcome"),
		Version:  polipage.Opt(envOr("POLI_PAGE_TEST_VERSION", "1.0.0")),
		Data:     map[string]any{"name": "Integration Test"},
	})
	if err != nil {
		t.Fatalf("Preview err = %v", err)
	}
	if res.HTML == "" {
		t.Error("Preview returned empty HTML")
	}
}

func integrationInput() polipage.ProjectModeInput {
	return polipage.ProjectModeInput{
		Project:  envOr("POLI_PAGE_TEST_PROJECT", "getting-started"),
		Template: envOr("POLI_PAGE_TEST_TEMPLATE", "welcome"),
		Version:  polipage.Opt(envOr("POLI_PAGE_TEST_VERSION", "1.0.0")),
		Data:     map[string]any{"name": "Integration Test"},
	}
}

func TestIntegration_Render_PDF_returnsValidPDFBytes(t *testing.T) {
	client := integrationClient(t)
	pdf, err := client.Render.PDF(context.Background(), integrationInput())
	if err != nil {
		t.Fatalf("PDF err = %v", err)
	}
	if len(pdf) < 1000 {
		t.Errorf("PDF size = %d bytes, want >= 1000", len(pdf))
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Errorf("PDF prefix = %q, want %%PDF magic bytes", string(pdf[:4]))
	}
}

func TestIntegration_Render_Document_returnsDescriptorWithDownloadablePDF(t *testing.T) {
	client := integrationClient(t)
	doc, err := client.Render.Document(context.Background(), integrationInput())
	if err != nil {
		t.Fatalf("Document err = %v", err)
	}
	if doc.DocumentID == "" {
		t.Error("DocumentID empty")
	}
	if doc.PageCount <= 0 {
		t.Errorf("PageCount = %d, want > 0", doc.PageCount)
	}
	if doc.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want > 0", doc.SizeBytes)
	}
	if doc.PresignedPDFURL == "" {
		t.Error("PresignedPDFURL empty")
	}
	pdf, err := doc.DownloadPDF(context.Background())
	if err != nil {
		t.Fatalf("DownloadPDF err = %v", err)
	}
	if len(pdf) < 1000 || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Errorf("DownloadPDF returned %d bytes, prefix=%q", len(pdf), string(pdf[:min(4, len(pdf))]))
	}
}

func TestIntegration_Render_PDFStream_streamsSameBytes(t *testing.T) {
	client := integrationClient(t)
	body, err := client.Render.PDFStream(context.Background(), integrationInput())
	if err != nil {
		t.Fatalf("PDFStream err = %v", err)
	}
	defer body.Close()
	streamed, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read stream err = %v", err)
	}
	if len(streamed) < 1000 || !bytes.HasPrefix(streamed, []byte("%PDF")) {
		t.Errorf("PDFStream returned %d bytes, prefix=%q", len(streamed), string(streamed[:min(4, len(streamed))]))
	}
}

func TestIntegration_Render_PDF_badAPIKeyReturnsAuthError(t *testing.T) {
	if os.Getenv("POLI_PAGE_API_KEY") == "" {
		t.Skip("POLI_PAGE_API_KEY not set — skipping integration test")
	}
	client := polipage.NewClient(
		option.WithAPIKey("pp_test_invalid_xxx"),
		option.WithBaseURL(envOr("POLI_PAGE_BASE_URL", "https://api-develop.poli.page")),
		option.WithMaxRetries(0),
	)
	_, err := client.Render.PDF(context.Background(), integrationInput())
	if err == nil {
		t.Fatal("PDF err = nil, want auth error")
	}
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || !pErr.IsAuthError() {
		t.Fatalf("err = %v, want IsAuthError() == true", err)
	}
}
