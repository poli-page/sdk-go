// Demo of the official Poli Page SDK for Go.
//
//	go run ./cmd/demo
//	# or, against develop:
//	POLI_PAGE_BASE_URL=https://api-develop.poli.page go run ./cmd/demo
//	# or remote-only, no checkout:
//	go run github.com/poli-page/sdk-go/cmd/demo@latest
//
// Walks every public method of the SDK in the order recommended for SDK
// porters (see sdk-node/demo/README.md "Notes for SDK porters") and
// writes the results under ./output/. Uses the auto-provisioned
// getting-started/welcome/1.0.0 template — no project setup required
// for any newcomer with a fresh pp_test_* key.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

const (
	envFileName    = ".env"
	defaultBaseURL = "https://api.poli.page"
	outputDir      = "output"
	totalSteps     = 10
)

// projectInput is the canonical demo input — getting-started/welcome is
// auto-provisioned in every Poli Page org.
func projectInput() polipage.ProjectModeInput {
	return polipage.ProjectModeInput{
		Project:  "getting-started",
		Template: "welcome",
		Version:  polipage.Opt("1.0.0"),
		Data:     map[string]any{"name": "SDK Demo"},
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("demo failed: %v", err)
	}
}

func run() error {
	apiKey, err := ensureAPIKey()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	client := polipage.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(resolveBaseURL()),
		option.WithOnRetry(func(e polipage.RetryEvent) {
			fmt.Printf("  %s retrying after %.0fms (attempt %d)\n", dim("↻"), e.DelayMs, e.Attempt)
		}),
	)
	ctx := context.Background()

	// 1. Render.PDF — buffered bytes.
	step(1, "Render.PDF — PDF bytes in memory")
	pdf, err := client.Render.PDF(ctx, projectInput())
	if err != nil {
		return fmt.Errorf("Render.PDF: %w", err)
	}
	if err := writeArtifact("render.pdf", pdf); err != nil {
		return err
	}
	fmt.Printf("  %d bytes, magic: %s\n", len(pdf), bold(string(pdf[:4])))

	// 2. Render.PDFStream — io.Copy to disk.
	step(2, "Render.PDFStream — io.ReadCloser piped to disk")
	body, err := client.Render.PDFStream(ctx, projectInput())
	if err != nil {
		return fmt.Errorf("Render.PDFStream: %w", err)
	}
	streamPath := filepath.Join(outputDir, "stream.pdf")
	f, err := os.Create(streamPath)
	if err != nil {
		_ = body.Close()
		return fmt.Errorf("create stream.pdf: %w", err)
	}
	n, copyErr := io.Copy(f, body)
	_ = f.Close()
	_ = body.Close()
	if copyErr != nil {
		return fmt.Errorf("stream copy: %w", copyErr)
	}
	fmt.Printf("  %d bytes streamed → %s\n", n, fileLink(streamPath))

	// 3. RenderToFile — convenience wrapper for the same flow.
	step(3, "RenderToFile — render straight to disk")
	filePath := filepath.Join(outputDir, "file.pdf")
	if err := polipage.RenderToFile(ctx, client, projectInput(), filePath); err != nil {
		return fmt.Errorf("RenderToFile: %w", err)
	}
	fmt.Printf("  wrote %s\n", fileLink(filePath))

	// 4. Render.Preview — paginated HTML (inline mode here, exercises both modes since project mode is already covered above).
	step(4, "Render.Preview — paginated HTML (inline mode)")
	preview, err := client.Render.Preview(ctx, polipage.InlineModeInput{
		Template: "<html><body><h1>{{ name }}</h1><p>Hello from inline mode</p></body></html>",
		Data:     map[string]any{"name": "SDK Demo"},
	})
	if err != nil {
		return fmt.Errorf("Render.Preview: %w", err)
	}
	if err := writeArtifactString("preview.html", preview.HTML); err != nil {
		return err
	}
	fmt.Printf("  %d total pages, %d chars HTML\n", preview.TotalPages, len(preview.HTML))

	// 5. Render.Document — render + store, return descriptor.
	step(5, "Render.Document — render + store, return descriptor")
	doc, err := client.Render.Document(ctx, projectInput())
	if err != nil {
		return fmt.Errorf("Render.Document: %w", err)
	}
	fmt.Printf("  documentId: %s\n", bold(doc.DocumentID))
	fmt.Printf("  pages=%d sizeBytes=%d environment=%s\n", doc.PageCount, doc.SizeBytes, doc.Environment)

	// 6. Documents.Get — fetch fresh descriptor (presigned URL reissued).
	step(6, "Documents.Get — fetch a fresh descriptor")
	fresh, err := client.Documents.Get(ctx, doc.DocumentID)
	if err != nil {
		return fmt.Errorf("Documents.Get: %w", err)
	}
	fmt.Printf("  documentId: %s expiresAt=%s\n", fresh.DocumentID, fresh.ExpiresAt)

	// 7. Documents.Thumbnails — tier-gated; tolerate THUMBNAILS_NOT_AVAILABLE on Free.
	step(7, "Documents.Thumbnails — generate thumbnails (tier-gated)")
	thumbs, err := client.Documents.Thumbnails(ctx, doc.DocumentID, polipage.ThumbnailOptions{
		Width:  320,
		Format: polipage.ThumbnailFormatPNG,
	})
	switch {
	case err == nil:
		fmt.Printf("  %d thumbnail(s), first contentType=%s\n", len(thumbs), thumbs[0].ContentType)
	case isErrCode(err, "THUMBNAILS_NOT_AVAILABLE"):
		fmt.Printf("  %s Free tier — thumbnails are gated. Upgrade to Starter+ to enable.\n", yellow("⚠"))
	default:
		return fmt.Errorf("Documents.Thumbnails: %w", err)
	}

	// 8. Documents.Preview — stored HTML + page count from response header.
	step(8, "Documents.Preview — stored HTML + page count")
	stored, err := client.Documents.Preview(ctx, doc.DocumentID)
	if err != nil {
		return fmt.Errorf("Documents.Preview: %w", err)
	}
	if err := writeArtifactString("document-preview.html", stored.HTML); err != nil {
		return err
	}
	fmt.Printf("  %d page(s), %d chars HTML\n", stored.PageCount, len(stored.HTML))

	// 9. Documents.Delete — soft-delete the document.
	step(9, "Documents.Delete — soft-delete the stored document")
	if err := client.Documents.Delete(ctx, doc.DocumentID); err != nil {
		return fmt.Errorf("Documents.Delete: %w", err)
	}
	fmt.Printf("  %s deleted %s\n", green("✔"), doc.DocumentID)

	// 10. Error path — deliberately trigger a 400.
	step(10, "Error path — deliberately trigger INVALID_VERSION_FORMAT")
	fmt.Printf("  %s This step is intentional — the SDK is about to fail.\n", yellow("⚠"))
	_, err = client.Render.PDF(ctx, polipage.ProjectModeInput{
		Project: "getting-started", Template: "welcome",
		Version: polipage.Opt("banana"),
		Data:    map[string]any{},
	})
	if err == nil {
		fmt.Printf("  %s unexpected: the bad call succeeded\n", red("✗"))
		return errors.New("expected error path to fail")
	}
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		return fmt.Errorf("expected *polipage.Error; got %T", err)
	}
	fmt.Printf("  %s caught polipage.Error via errors.As:\n", green("✔"))
	fmt.Printf("    code=%s statusCode=%d requestId=%s\n", pErr.Code, pErr.StatusCode, pErr.RequestID)
	fmt.Printf("    isAuthError=%v isValidationError=%v isRetryable=%v\n",
		pErr.IsAuthError(), pErr.IsValidationError(), pErr.IsRetryable())

	fmt.Printf("\n%s %s Inspect %s\n\n", green("✔"), bold("All steps completed."), fileLink(outputDir))
	return nil
}

// ─── small helpers ────────────────────────────────────────────────────

func writeArtifact(name string, content []byte) error {
	dst := filepath.Join(outputDir, name)
	if err := os.WriteFile(dst, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

func writeArtifactString(name, content string) error {
	return writeArtifact(name, []byte(content))
}

func isErrCode(err error, code string) bool {
	var pErr *polipage.Error
	return errors.As(err, &pErr) && pErr.Code == code
}

// ─── API-key resolution ───────────────────────────────────────────────

// ensureAPIKey reads POLI_PAGE_API_KEY in order: shell env, ./.env file,
// interactive prompt. On a successful prompt the key is appended to .env
// so future runs are silent. Mirrors sdk-node/demo/_shared.mjs.
func ensureAPIKey() (string, error) {
	if v := strings.TrimSpace(os.Getenv("POLI_PAGE_API_KEY")); v != "" {
		return v, nil
	}
	envVars, _ := readEnvFile(envFileName)
	if v := strings.TrimSpace(envVars["POLI_PAGE_API_KEY"]); v != "" {
		fmt.Printf("  %s using POLI_PAGE_API_KEY from %s\n", dim("·"), envFileName)
		return v, nil
	}
	return promptForAPIKey()
}

func promptForAPIKey() (string, error) {
	rule := dim("  " + strings.Repeat("─", 65))
	fmt.Println()
	fmt.Println(rule)
	fmt.Println(yellow(bold("   No POLI_PAGE_API_KEY found.")))
	fmt.Println(rule)
	fmt.Println()
	fmt.Println("   This demo needs a test key (" + cyan("pp_test_*") + ") to talk to")
	fmt.Println("   the Poli Page API. Test keys never bill or send real documents.")
	fmt.Println()
	fmt.Println(bold("   How to get one:"))
	fmt.Println("     1. Sign in at " + cyan("https://app.poli.page"))
	fmt.Println("     2. Go to your organization's API keys page:")
	fmt.Println("          " + cyan("https://app.poli.page/orgs/{YOUR_ORG}/keys"))
	fmt.Println(dim("        (replace {YOUR_ORG} with your org slug — visible in the"))
	fmt.Println(dim("         dashboard URL once you're signed in)"))
	fmt.Println("     3. Click \"Create key\" and copy the value (starts with " + cyan("pp_test_") + ").")
	fmt.Println()
	fmt.Println("   Paste it below — we'll save it to " + cyan(envFileName) + " so future runs")
	fmt.Println("   pick it up automatically. (You can also set " + dim("POLI_PAGE_API_KEY") +
		" in your")
	fmt.Println("   shell — that wins over the file.)")
	fmt.Println()
	fmt.Print(bold("   Paste your pp_test_* key") + " (or Ctrl-C to cancel): ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read key: %w", err)
	}
	key := strings.TrimSpace(line)
	if !strings.HasPrefix(key, "pp_test_") {
		return "", fmt.Errorf("expected a key starting with pp_test_; got %q", key)
	}
	if err := appendEnvFile(envFileName, "POLI_PAGE_API_KEY", key); err != nil {
		return "", fmt.Errorf("save key to %s: %w", envFileName, err)
	}
	fmt.Printf("  %s saved to %s\n\n", green("✔"), cyan(envFileName))
	return key, nil
}

func resolveBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("POLI_PAGE_BASE_URL")); v != "" {
		return v
	}
	envVars, _ := readEnvFile(envFileName)
	if v := strings.TrimSpace(envVars["POLI_PAGE_BASE_URL"]); v != "" {
		return v
	}
	return defaultBaseURL
}

func readEnvFile(path string) (map[string]string, error) {
	result := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = strings.TrimPrefix(strings.TrimSuffix(val, `"`), `"`)
		val = strings.TrimPrefix(strings.TrimSuffix(val, `'`), `'`)
		result[key] = val
	}
	return result, scanner.Err()
}

func appendEnvFile(path, key, value string) error {
	existing, _ := os.ReadFile(path)
	leadingNL := ""
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		leadingNL = "\n"
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "%s%s=%s\n", leadingNL, key, value)
	return err
}

// ─── tiny ANSI helpers (no deps) ──────────────────────────────────────

// useColor disables coloring when stdout isn't a TTY (NO_COLOR also wins).
// We can't easily detect a TTY without bringing in golang.org/x/term, so
// we trust the NO_COLOR convention only; modern terminals render ANSI
// fine and pipes generally swallow the escapes harmlessly.
func useColor() bool {
	return os.Getenv("NO_COLOR") == ""
}

func wrap(code, s string) string {
	if !useColor() {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string   { return wrap("1", s) }
func dim(s string) string    { return wrap("2", s) }
func red(s string) string    { return wrap("31", s) }
func green(s string) string  { return wrap("32", s) }
func yellow(s string) string { return wrap("33", s) }
func cyan(s string) string   { return wrap("36", s) }

func step(n int, name string) {
	fmt.Printf("\n%s\n", cyan(bold(fmt.Sprintf("[%d/%d] %s", n, totalSteps, name))))
}

func fileLink(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return cyan(path)
	}
	return cyan("file://" + abs)
}
