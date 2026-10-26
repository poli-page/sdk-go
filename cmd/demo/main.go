// Demo of the official Poli Page SDK for Go.
//
//	go run ./cmd/demo
//	# or remote-only, no checkout:
//	go run github.com/poli-page/sdk-go/cmd/demo@latest
//
// It reads POLI_PAGE_API_KEY (a pp_sandbox_ key) and the optional
// POLI_PAGE_BASE_URL from the shell or ./.env, and asks for the key when
// neither has it. It renders the welcome template of the getting-started
// project, which every organization gets published in its sandbox
// environment, then runs nine steps and writes the results under ./output/:
//
//  1. Render.PDF
//  2. Render.PDFStream
//  3. RenderToFile
//  4. Render.Document
//  5. Documents.Get
//  6. Documents.Thumbnails
//  7. Documents.Preview
//  8. Documents.Delete
//  9. not-found: renders template does-not-exist, expects 404
//     TEMPLATE_NOT_FOUND and prints the error's code, status, message and
//     request id; any other result fails the demo
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
	"regexp"
	"strings"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

const (
	envFileName = ".env"
	outputDir   = "output"
	totalSteps  = 9
)

// apiKeyPattern is the contract's full-key regex
// ^pp_(?=[a-z][a-z0-9-]{0,19}_)[a-z][a-z0-9]*(?:-[a-z0-9]+)*_[0-9a-f]{32}$
// without its lookahead, which Go's RE2 engine does not support. isAPIKey
// applies the lookahead's 20-character limit to the captured environment
// name instead.
var apiKeyPattern = regexp.MustCompile(`^pp_([a-z][a-z0-9]*(?:-[a-z0-9]+)*)_[0-9a-f]{32}$`)

// isAPIKey reports whether s has the shape of a Poli Page API key. It does
// not check which environment the key names.
func isAPIKey(s string) bool {
	m := apiKeyPattern.FindStringSubmatch(s)
	return m != nil && len(m[1]) <= 20
}

func projectInput() polipage.RenderInput {
	return polipage.RenderInput{
		Project:  "getting-started",
		Template: "welcome",
		Data:     map[string]any{"name": "SDK Demo"},
	}
}

// notFoundInput names a template that does not exist. Data is an empty map,
// not nil, so the body carries "data":{} rather than null.
func notFoundInput() polipage.RenderInput {
	return polipage.RenderInput{
		Project:  "getting-started",
		Template: "does-not-exist",
		Data:     map[string]any{},
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

	// 4. Render.Document — render + store, return descriptor.
	step(4, "Render.Document — render + store, return descriptor")
	doc, err := client.Render.Document(ctx, projectInput())
	if err != nil {
		return fmt.Errorf("Render.Document: %w", err)
	}
	fmt.Printf("  documentId: %s\n", bold(doc.DocumentID))
	fmt.Printf("  pages=%d sizeBytes=%d environment=%s\n", doc.PageCount, doc.SizeBytes, doc.Environment)

	// 5. Documents.Get — fetch fresh descriptor (presigned URL reissued).
	step(5, "Documents.Get — fetch a fresh descriptor")
	fresh, err := client.Documents.Get(ctx, doc.DocumentID)
	if err != nil {
		return fmt.Errorf("Documents.Get: %w", err)
	}
	fmt.Printf("  documentId: %s expiresAt=%s\n", fresh.DocumentID, fresh.ExpiresAt)

	// 6. Documents.Thumbnails — tier-gated; tolerate THUMBNAILS_NOT_AVAILABLE on Free.
	step(6, "Documents.Thumbnails — generate thumbnails (tier-gated)")
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

	// 7. Documents.Preview — stored HTML + page count from response header.
	step(7, "Documents.Preview — stored HTML + page count")
	stored, err := client.Documents.Preview(ctx, doc.DocumentID)
	if err != nil {
		return fmt.Errorf("Documents.Preview: %w", err)
	}
	if err := writeArtifactString("document-preview.html", stored.HTML); err != nil {
		return err
	}
	fmt.Printf("  %d page(s), %d chars HTML\n", stored.PageCount, len(stored.HTML))

	// 8. Documents.Delete — soft-delete the document.
	step(8, "Documents.Delete — soft-delete the stored document")
	if err := client.Documents.Delete(ctx, doc.DocumentID); err != nil {
		return fmt.Errorf("Documents.Delete: %w", err)
	}
	fmt.Printf("  %s deleted %s\n", green("✔"), doc.DocumentID)

	// 9. not-found — the API answers 404 TEMPLATE_NOT_FOUND.
	step(9, "not-found — render a template that does not exist")
	fmt.Printf("  %s This step is meant to fail.\n", yellow("⚠"))
	_, err = client.Render.PDF(ctx, notFoundInput())
	pErr, err := notFoundError(err)
	if err != nil {
		fmt.Printf("  %s %v\n", red("✗"), err)
		return err
	}
	fmt.Printf("  %s caught *polipage.Error:\n", green("✔"))
	fmt.Printf("    code:      %s\n", pErr.Code)
	fmt.Printf("    status:    %d\n", pErr.StatusCode)
	fmt.Printf("    message:   %s\n", pErr.Message)
	fmt.Printf("    requestId: %s\n", pErr.RequestID)

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

// notFoundError returns err as a *polipage.Error when it is the 404
// TEMPLATE_NOT_FOUND the not-found step expects, and an error otherwise, so
// a network failure or any other API error fails the demo.
func notFoundError(err error) (*polipage.Error, error) {
	if err == nil {
		return nil, errors.New("not-found: expected 404 TEMPLATE_NOT_FOUND, but the render succeeded")
	}
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.StatusCode != 404 || pErr.Code != polipage.ErrCodeTemplateNotFound {
		return nil, fmt.Errorf("not-found: expected 404 TEMPLATE_NOT_FOUND, got: %w", err)
	}
	return pErr, nil
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
	fmt.Println("   This demo needs a sandbox key (" + cyan("pp_sandbox_*") + ") to talk to")
	fmt.Println("   the Poli Page API. Sandbox renders are watermarked, do not count")
	fmt.Println("   toward your monthly quota and expire after 30 days.")
	fmt.Println()
	fmt.Println("   It renders the getting-started project that every organization gets")
	fmt.Println("   in its sandbox. If yours renamed, deleted or unpublished it, use a")
	fmt.Println("   fresh organization.")
	fmt.Println()
	fmt.Println(bold("   How to get one:"))
	fmt.Println("     1. Sign in at " + cyan("https://app.poli.page"))
	fmt.Println("     2. Go to your organization's API keys page:")
	fmt.Println("          " + cyan("https://app.poli.page/orgs/{YOUR_ORG}/keys"))
	fmt.Println(dim("        (replace {YOUR_ORG} with your org slug — visible in the"))
	fmt.Println(dim("         dashboard URL once you're signed in)"))
	fmt.Println("     3. Click \"Create key\" and copy the value (starts with " + cyan("pp_sandbox_") + ").")
	fmt.Println()
	fmt.Println("   Paste it below — we'll save it to " + cyan(envFileName) + " so future runs")
	fmt.Println("   pick it up automatically. (You can also set " + dim("POLI_PAGE_API_KEY") +
		" in your")
	fmt.Println("   shell — that wins over the file.)")
	fmt.Println()
	fmt.Print(bold("   Paste your pp_sandbox_* key") + " (or Ctrl-C to cancel): ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read key: %w", err)
	}
	key := strings.TrimSpace(line)
	if key == "" {
		return "", errors.New("no key entered")
	}
	if !isAPIKey(key) {
		return "", errors.New("that is not a Poli Page API key: expected pp_{environment}_ followed by 32 lowercase hex characters")
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
	return polipage.DefaultBaseURL
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
