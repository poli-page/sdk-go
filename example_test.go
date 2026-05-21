package polipage_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"time"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// Most examples in this file omit the `// Output:` directive. pkg.go.dev
// still renders them as runnable code blocks alongside the doc comments;
// the absence of an Output line just tells `go test` to compile-check
// them rather than diff their stdout against an expected value. That is
// the right trade-off for examples that hit a live API.

// ExampleNewClient demonstrates the canonical construction of a Client.
// The API key is the only required option; sensible defaults (3 attempts
// total, 500ms base retry delay, 60s per-request timeout) apply to
// everything else.
func ExampleNewClient() {
	client := polipage.NewClient(
		option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
		option.WithMaxRetries(3),
	)
	// Share one client across the process — *http.Client pools
	// connections automatically.
	_ = client
}

// ExampleRender_PDF renders a project-mode document into a []byte. Two
// HTTP calls under the hood; one from the caller's point of view.
func ExampleRender_PDF() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	pdf, err := client.Render.PDF(context.Background(), polipage.ProjectModeInput{
		Project:  "billing",
		Template: "invoice",
		Version:  polipage.Opt("1.0.0"),
		Data:     map[string]any{"invoiceNumber": "INV-001"},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("PDF size: %d bytes\n", len(pdf))
}

// ExampleRender_PDF_withIdempotencyKey supplies a natural identifier as
// the Idempotency-Key so retried requests against the same logical
// invoice never produce duplicate documents server-side.
func ExampleRender_PDF_withIdempotencyKey() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	_, _ = client.Render.PDF(
		context.Background(),
		polipage.ProjectModeInput{
			Project: "billing", Template: "invoice",
			Version: polipage.Opt("1.0.0"),
			Data:    map[string]any{"invoiceNumber": "INV-001"},
		},
		option.WithIdempotencyKey("inv-INV-001"),
	)
}

// ExampleRender_PDFStream streams PDF bytes directly to a destination
// without buffering — memory stays bounded regardless of document size.
// The caller MUST close the returned ReadCloser.
func ExampleRender_PDFStream() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	body, err := client.Render.PDFStream(context.Background(), polipage.ProjectModeInput{
		Project: "billing", Template: "invoice",
		Version: polipage.Opt("1.0.0"),
		Data:    map[string]any{"invoiceNumber": "INV-001"},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = body.Close() }()

	out, _ := os.Create("invoice.pdf")
	defer func() { _ = out.Close() }()
	_, _ = io.Copy(out, body)
}

// ExampleRender_Preview generates paginated HTML. Unlike PDF/PDFStream/
// Document, Preview accepts both [polipage.ProjectModeInput] and
// [polipage.InlineModeInput] — useful for validating raw templates
// before saving them.
func ExampleRender_Preview() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	res, err := client.Render.Preview(context.Background(), polipage.InlineModeInput{
		Template: "<html><body><h1>{{ name }}</h1></body></html>",
		Data:     map[string]any{"name": "Mickael"},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d page(s), %d chars HTML\n", res.TotalPages, len(res.HTML))
}

// ExampleRender_Document stores the document server-side and returns its
// descriptor without auto-fetching the PDF bytes. Use when you want to
// persist the documentId for later access (preview, thumbnails,
// re-download) without paying for the bytes up front.
func ExampleRender_Document() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	doc, err := client.Render.Document(context.Background(), polipage.ProjectModeInput{
		Project: "billing", Template: "invoice",
		Version: polipage.Opt("1.0.0"),
		Data:    map[string]any{"invoiceNumber": "INV-001"},
		// Metadata is echoed back on Documents.Get — primitives only.
		Metadata: polipage.RenderMetadata{"customerId": "cust_123"},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("documentId=%s pages=%d\n", doc.DocumentID, doc.PageCount)
}

// ExampleDocuments_Get retrieves a stored document with a fresh presigned
// URL. The PDF download URL has a ~15-minute TTL — call Get again to
// reissue it once expired.
func ExampleDocuments_Get() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	doc, err := client.Documents.Get(context.Background(), "doc_abc123")
	if err != nil {
		log.Fatal(err)
	}
	pdf, err := doc.DownloadPDF(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d bytes\n", len(pdf))
}

// ExampleDocuments_Preview retrieves the stored HTML for a document. The
// engine performs no work on this call — it's a cheap read suitable for
// preview UIs and snapshot tests in CI.
func ExampleDocuments_Preview() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	res, err := client.Documents.Preview(context.Background(), "doc_abc123")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d page(s), %d chars HTML\n", res.PageCount, len(res.HTML))
}

// ExampleDocuments_Thumbnails generates page thumbnails. Pass
// [polipage.ThumbnailFormatJPEG] with a Quality value for JPEG output;
// otherwise PNG is returned. Tier-gated on the API — Free tier returns
// THUMBNAILS_NOT_AVAILABLE.
func ExampleDocuments_Thumbnails() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	thumbs, err := client.Documents.Thumbnails(
		context.Background(),
		"doc_abc123",
		polipage.ThumbnailOptions{
			Width:  840,
			Format: polipage.ThumbnailFormatPNG,
			Pages:  []int{1, 2, 3},
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	for _, t := range thumbs {
		fmt.Printf("page %d: %dx%d %s\n", t.Page, t.Width, t.Height, t.ContentType)
	}
}

// ExampleDocuments_Delete soft-deletes a stored document. Subsequent
// reads of the same documentId surface as [polipage.ErrGone].
func ExampleDocuments_Delete() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	if err := client.Documents.Delete(context.Background(), "doc_abc123"); err != nil {
		log.Fatal(err)
	}
}

// ExampleDocumentDescriptor_DownloadPDF fetches the bytes from a
// descriptor's PresignedPDFURL. Useful when you persisted the descriptor
// earlier and want to defer the byte fetch until needed.
func ExampleDocumentDescriptor_DownloadPDF() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	doc, err := client.Documents.Get(context.Background(), "doc_abc123")
	if err != nil {
		log.Fatal(err)
	}
	pdf, err := doc.DownloadPDF(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	_ = os.WriteFile("invoice.pdf", pdf, 0o644)
}

// ExampleRenderToFile streams a PDF directly to disk. Parent directories
// are created if missing; existing files are overwritten. Memory stays
// bounded regardless of document size.
func ExampleRenderToFile() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))
	err := polipage.RenderToFile(
		context.Background(),
		client,
		polipage.ProjectModeInput{
			Project: "billing", Template: "invoice",
			Version: polipage.Opt("1.0.0"),
			Data:    map[string]any{"invoiceNumber": "INV-001"},
		},
		"out/invoice.pdf",
	)
	if err != nil {
		log.Fatal(err)
	}
}

// ExampleOpt shows the common use of the Opt helper: turning a literal
// value into a *T for optional pointer-typed fields like
// [polipage.ProjectModeInput.Version].
func ExampleOpt() {
	in := polipage.ProjectModeInput{
		Project:  "billing",
		Template: "invoice",
		Version:  polipage.Opt("1.0.0"),
		Data:     map[string]any{},
	}
	fmt.Println(*in.Version)
	// Output: 1.0.0
}

// ExampleError_typedInspection demonstrates the two idiomatic ways to
// branch on errors returned by the SDK: errors.Is against the
// package-level sentinels for quick checks, and errors.As to inspect
// the full *Error value (Code, StatusCode, RequestID, Cause).
func ExampleError_typedInspection() {
	client := polipage.NewClient(option.WithAPIKey("pp_test_bad_key"))
	_, err := client.Render.PDF(context.Background(), polipage.ProjectModeInput{
		Project: "x", Template: "y", Data: map[string]any{},
	})
	if errors.Is(err, polipage.ErrUnauthorized) {
		// refresh credentials, surface to the user, ...
	}
	var pErr *polipage.Error
	if errors.As(err, &pErr) {
		fmt.Printf("code=%s status=%d requestID=%s\n", pErr.Code, pErr.StatusCode, pErr.RequestID)
	}
}

// ExampleNewClient_withTimeout shows the recommended cancellation /
// deadline pattern: a per-call context.WithTimeout. The Client's own
// WithTimeout is a fallback applied only when the caller's ctx has no
// deadline.
func ExampleNewClient_withTimeout() {
	client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _ = client.Render.PDF(ctx, polipage.ProjectModeInput{
		Project: "billing", Template: "invoice",
		Version: polipage.Opt("1.0.0"),
		Data:    map[string]any{},
	})
}

// ExampleNewClient_withLogger wires a structured slog logger. The SDK
// emits one DEBUG line per HTTP attempt, one WARN per retry, and one
// ERROR per terminal failure. Headers and bodies that could contain the
// API key are not logged.
func ExampleNewClient_withLogger() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client := polipage.NewClient(
		option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
		option.WithLogger(logger),
	)
	_ = client
}
