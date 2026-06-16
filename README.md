# Poli Page SDK for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/poli-page/sdk-go.svg)](https://pkg.go.dev/github.com/poli-page/sdk-go)
[![docs](https://img.shields.io/badge/docs-online-brightgreen)](https://poli-page.github.io/sdk-go/)
[![Release](https://img.shields.io/github/v/release/poli-page/sdk-go?display_name=tag&sort=semver)](https://github.com/poli-page/sdk-go/releases)
[![CI](https://github.com/poli-page/sdk-go/actions/workflows/ci.yml/badge.svg)](https://github.com/poli-page/sdk-go/actions/workflows/ci.yml)
[![CodeQL](https://github.com/poli-page/sdk-go/actions/workflows/codeql.yml/badge.svg)](https://github.com/poli-page/sdk-go/actions/workflows/codeql.yml)
[![codecov](https://codecov.io/gh/poli-page/sdk-go/branch/main/graph/badge.svg)](https://codecov.io/gh/poli-page/sdk-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/poli-page/sdk-go)](https://goreportcard.com/report/github.com/poli-page/sdk-go)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![lint: golangci-lint](https://img.shields.io/badge/lint-golangci--lint-00ADD8?logo=go&logoColor=white)](https://golangci-lint.run/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Official Go SDK for [Poli Page](https://poli.page) — render polished PDFs from HTML templates via the Poli Page API.

→ Documentation: **<https://poli-page.github.io/sdk-go/>**

→ API reference (auto-generated from doc comments): **[pkg.go.dev/github.com/poli-page/sdk-go](https://pkg.go.dev/github.com/poli-page/sdk-go)**

## Install

```bash
go get github.com/poli-page/sdk-go
```

Requires Go 1.25 or later. Zero runtime dependencies.

## Quick start

### Project mode — render a published template by slug

```go
package main

import (
    "context"
    "log"
    "os"

    polipage "github.com/poli-page/sdk-go"
    "github.com/poli-page/sdk-go/option"
)

func main() {
    client := polipage.NewClient(option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")))

    pdf, err := client.Render.PDF(context.Background(), polipage.ProjectModeInput{
        Project:  "getting-started",
        Template: "welcome",
        Version:  polipage.Opt("1.0.0"),
        Data:     map[string]any{"name": "World"},
    })
    if err != nil {
        log.Fatal(err)
    }
    _ = os.WriteFile("welcome.pdf", pdf, 0o644)
}
```

Every Poli Page org comes pre-provisioned with a `getting-started/welcome` template, so the snippet above runs as-is the moment you have an API key — no project setup needed. For your own templates, swap the slugs once you've pushed a version with the `poli` CLI:

```go
pdf, err := client.Render.PDF(ctx, polipage.ProjectModeInput{
    Project:  "billing",
    Template: "invoice",
    Version:  polipage.Opt("1.0.0"),
    Data:     map[string]any{"invoiceNumber": "INV-001", "total": 1280},
})
```

### Preview inline HTML

`Render.Preview` accepts raw HTML for live editing and visual inspection without producing a stored document. Use this for editor previews or layout tests.

```go
res, err := client.Render.Preview(ctx, polipage.InlineModeInput{
    Template: "<h1>Hello {{ name }}</h1>",
    Data:     map[string]any{"name": "World"},
})
// res.HTML, res.TotalPages, res.Environment
```

**`Render.PDF`, `Render.PDFStream`, and `Render.Document` require project mode** — they accept [`ProjectModeInput`](https://pkg.go.dev/github.com/poli-page/sdk-go#ProjectModeInput) directly so inline-mode calls are rejected at compile time. `Render.Preview` accepts the [`RenderInput`](https://pkg.go.dev/github.com/poli-page/sdk-go#RenderInput) sealed interface, satisfied by both modes.

### Write a PDF to disk

```go
err := polipage.RenderToFile(ctx, client, polipage.ProjectModeInput{
    Project:  "getting-started",
    Template: "welcome",
    Version:  polipage.Opt("1.0.0"),
    Data:     map[string]any{"name": "World"},
}, "./welcome.pdf")
```

`RenderToFile` streams response bytes directly to disk (bounded memory). Parent directories are created on demand; existing files are overwritten.

### Try it locally — runnable demo

The repo ships a runnable demo that exercises every public method end-to-end against the real API:

```bash
go run ./cmd/demo
# or remote-only, no checkout:
go run github.com/poli-page/sdk-go/cmd/demo@latest
```

First run prompts for a `pp_test_*` key and saves it to `.env`. Subsequent runs are silent. The demo walks through `Render.PDF` → `PDFStream` → `RenderToFile` → `Preview` → `Document` → `Documents.Get` → `Thumbnails` → `Preview` → `Delete` → an intentional error path, writing artifacts to `./output/`.

### Stream — for large PDFs or piping to S3 / HTTP responses

```go
body, err := client.Render.PDFStream(ctx, polipage.ProjectModeInput{
    Project: "billing", Template: "invoice",
    Version: polipage.Opt("1.0.0"),
    Data:    map[string]any{"invoiceNumber": "INV-001"},
})
if err != nil {
    return err
}
defer body.Close()

// Pipe straight to an HTTP response, an S3 upload, a file — anything that
// accepts io.Reader. Memory stays bounded regardless of document size.
_, err = io.Copy(w, body)
```

## Working with stored documents

Every render produces a stored document, accessible via `documentId` for later download or thumbnails. `Render.PDF` and `Render.PDFStream` are conveniences that chain a presigned-URL fetch internally to return bytes; `Render.Document` returns just the descriptor (skip the auto-download when you'll fetch the bytes later).

```go
// 1. Render and store
doc, err := client.Render.Document(ctx, polipage.ProjectModeInput{
    Project:  "billing",
    Template: "invoice",
    Version:  polipage.Opt("1.0.0"),
    Data:     map[string]any{"invoiceNumber": "INV-001"},
    Metadata: polipage.RenderMetadata{"customerId": "cust_123"}, // your own audit data
})
// doc.DocumentID, doc.PageCount, doc.SizeBytes, doc.PresignedPDFURL, doc.Metadata, ...

// 2. Save doc.DocumentID in your database
_ = db.Invoices.Update("INV-001", doc.DocumentID)

// 3. Later, fetch a fresh presigned URL + download
fresh, err := client.Documents.Get(ctx, doc.DocumentID)
pdf, err := fresh.DownloadPDF(ctx)

// 4. Generate thumbnails
thumbs, err := client.Documents.Thumbnails(ctx, doc.DocumentID, polipage.ThumbnailOptions{
    Width:  320,
    Format: polipage.ThumbnailFormatPNG,
})

// 5. When done, soft-delete
err = client.Documents.Delete(ctx, doc.DocumentID)
```

The presigned URL has a ~15-minute TTL. If `DownloadPDF` fails with `Code: ErrCodeDownloadFailed` (HTTP 403 from S3), call `Documents.Get(ctx, id)` to refresh and retry.

## Authentication & environments

The mode is determined by the API key prefix:

- `pp_test_…` → sandbox mode (not billed, generous rate limits)
- `pp_live_…` → live mode (billed, production rate limits)
- `pp_sa_…`   → service-account keys; environment matches the SA's configuration (sandbox or live)

All prefixes hit the same endpoint (`https://api.poli.page`). The SDK passes the key through as a Bearer token and never inspects the prefix — pick whichever fits your deploy model.

## Methods

| Method                                                | Returns                              | Description |
| ----------------------------------------------------- | ------------------------------------ | ----------- |
| `client.Render.PDF(ctx, in, opts...)`                 | `([]byte, error)`                    | Render a PDF, return bytes |
| `client.Render.PDFStream(ctx, in, opts...)`           | `(io.ReadCloser, error)`             | Render and stream the response |
| `client.Render.Preview(ctx, in, opts...)`             | `(*PreviewResult, error)`            | Paginated HTML preview |
| `client.Render.Document(ctx, in, opts...)`            | `(*DocumentDescriptor, error)`       | Render and return descriptor (skip auto-download) |
| `client.Documents.Get(ctx, id)`                       | `(*DocumentDescriptor, error)`       | Retrieve a stored document |
| `client.Documents.Preview(ctx, id)`                   | `(*DocumentPreviewResult, error)`    | Stored document's paginated HTML |
| `client.Documents.Thumbnails(ctx, id, options, ...)`  | `([]Thumbnail, error)`               | Page thumbnails (PNG/JPEG, base64) |
| `client.Documents.Delete(ctx, id)`                    | `error`                              | Soft-delete a stored document |
| `(*DocumentDescriptor).DownloadPDF(ctx)`              | `([]byte, error)`                    | Fetch bytes from the descriptor's presigned URL |
| `polipage.RenderToFile(ctx, client, in, path)`        | `error`                              | Render and stream to disk |

## Configuration

All options live in the [`option`](https://pkg.go.dev/github.com/poli-page/sdk-go/option) subpackage and are passed to `NewClient` (variadic). Per-call options (currently only `WithIdempotencyKey`) are passed to individual methods as the final variadic argument.

| Option                                | Type                          | Default                  | Description |
| ------------------------------------- | ----------------------------- | ------------------------ | ----------- |
| `option.WithAPIKey(key)`               | `string`                      | (required)              | `pp_test_*` or `pp_live_*` API key |
| `option.WithBaseURL(url)`              | `string`                      | `https://api.poli.page` | API base URL |
| `option.WithMaxRetries(n)`             | `int`                         | `2`                     | Retry budget on top of the initial attempt |
| `option.WithRetryDelay(d)`             | `time.Duration`               | `500 * time.Millisecond`| Base exponential-backoff delay |
| `option.WithTimeout(d)`                | `time.Duration`               | `60 * time.Second`      | Per-request fallback deadline (when ctx has none) |
| `option.WithHTTPClient(c)`             | `*http.Client`                | new client               | Inject custom transport / TLS / middleware |
| `option.WithLogger(l)`                 | `*slog.Logger`                | discard                  | One DEBUG/attempt, WARN/retry, ERROR/terminal |
| `option.WithOnRetry(fn)`               | `func(polipage.RetryEvent)`   | nil                      | Fires before each retry sleep |
| `option.WithOnError(fn)`               | `func(error)`                 | nil                      | Fires on terminal failure |
| `option.WithIdempotencyKey(key)`       | `string`                      | UUID4                    | **Per-call** — override the auto-generated key |
| `option.WithRequestTimeout(d)`         | `time.Duration`               | `WithTimeout` value      | **Per-call** — override per-request deadline (no-op if ctx has a deadline) |
| `option.WithHeader(key, value)`        | `string, string`              | —                        | **Construction + per-call** — extra request header; per-call wins on duplicate keys |

## Error handling

The SDK returns a single error type, `*polipage.Error`, for every failure. Use [`errors.Is`](https://pkg.go.dev/errors#Is) against the package-level sentinels for quick branching, or [`errors.As`](https://pkg.go.dev/errors#As) to inspect the full value:

```go
import (
    "errors"
    "log"

    polipage "github.com/poli-page/sdk-go"
)

pdf, err := client.Render.PDF(ctx, in)
if err != nil {
    if errors.Is(err, polipage.ErrUnauthorized)  { return refreshCredentials() }
    if errors.Is(err, polipage.ErrRateLimit)     { return queueForLater() }
    if errors.Is(err, polipage.ErrNotFound)      { return show404() }

    var pErr *polipage.Error
    if errors.As(err, &pErr) {
        if pErr.IsValidationError() { log.Println("bad input:", pErr.Message) }
        if pErr.IsNetworkError()    { log.Println("network/timeout") }
        if pErr.IsRetryable()       { /* SDK already retried up to MaxRetries */ }
        log.Printf("code=%s status=%d requestId=%s\n", pErr.Code, pErr.StatusCode, pErr.RequestID)
    }
    return err
}
```

For lifecycle and billing failures, route the user to actionable messages rather than treating them as opaque errors:

```go
var pErr *polipage.Error
if errors.As(err, &pErr) {
    switch pErr.Code {
    case polipage.ErrCodePaymentRequired:       return showBanner("Subscription has unpaid invoices.")
    case polipage.ErrCodeOrganizationCancelled: return showBanner("Subscription cancelled — service is read-only.")
    case polipage.ErrCodeOrganizationPurged:    return showBanner("Organization has been purged.")
    case polipage.ErrCodeDocumentNotFound:      return show404()
    case polipage.ErrCodeGone:                  return show410() // document was soft-deleted
    }
}
```

## Cancellation

Pass a `context.Context` with a deadline or `cancel` to abort a render in flight:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

pdf, err := client.Render.PDF(ctx, in)
```

When the context is canceled, the SDK returns `*polipage.Error` with `Code == ErrCodeAborted`; when the deadline expires, `Code == ErrCodeTimeout`. Caller-aborted errors are never retried.

## Observability

The SDK ships with `log/slog` integration as the primary observability mechanism:

```go
logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
client := polipage.NewClient(
    option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
    option.WithLogger(logger),
)
```

One DEBUG line per HTTP attempt, one WARN per retry, one ERROR per terminal failure. Headers and bodies that could contain the API key are never logged.

For SDK-level events that don't fit a log line, register hooks:

```go
client := polipage.NewClient(
    option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
    option.WithOnRetry(func(e polipage.RetryEvent) {
        metrics.Counter("poli.retry").With("attempt", e.Attempt).Inc()
    }),
    option.WithOnError(func(err error) {
        sentry.CaptureException(err)
    }),
)
```

Hooks are synchronous, optional, and panic-safe — a hook that panics never breaks the request. For request/response inspection (full headers + body), inject a custom `*http.Client` via `option.WithHTTPClient` and add a middleware transport there — see the next section.

### Middleware via `http.RoundTripper`

The SDK's hooks fire at SDK-level events (retry, terminal error). For full-fidelity HTTP middleware — tracing, request signing, response caching, fixture recording, request/response logging with bodies — wrap a `http.RoundTripper` and pass it via `option.WithHTTPClient`. This is the stdlib idiom; the SDK does nothing special to enable it.

```go
type tracingTransport struct {
    base http.RoundTripper
}

func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    start := time.Now()
    resp, err := t.base.RoundTrip(req)
    log.Printf("%s %s → %d in %s", req.Method, req.URL.Path, resp.StatusCode, time.Since(start))
    return resp, err
}

client := polipage.NewClient(
    option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
    option.WithHTTPClient(&http.Client{
        Transport: &tracingTransport{base: http.DefaultTransport},
    }),
)
```

Compose multiple layers by chaining `RoundTripper`s (each wraps the previous one's `base`). The SDK's retry loop runs *outside* this transport, so each `RoundTrip` corresponds to one attempt — log every retry by counting `RoundTrip` calls per request.

### Per-call tracing IDs

For one-off per-request headers (a tracing ID, a tenant override, a feature flag), use `option.WithHeader` as a per-call option without rebuilding the client:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

pdf, err := client.Render.PDF(ctx, in,
    option.WithHeader("X-Trace-Id", traceID),
    option.WithRequestTimeout(5*time.Second), // overrides client default for this call only
)
```

Per-call header keys win over construction-time `WithHeader` entries for the same key; both override the SDK's own headers (`Authorization`, `Content-Type`, etc.) if you supply matching keys — caller's responsibility.

## Retries & idempotency

The SDK retries on **5xx**, **429**, **network errors**, and **timeouts**. Backoff is exponential (`RetryDelay × 2^N`) with jitter in `[0.5, 1.5)`, capped at the server's `Retry-After` header when provided (max 30s). Every POST sends an auto-generated `Idempotency-Key` (UUID v4); pass `option.WithIdempotencyKey("…")` as the last argument to override:

```go
_, err := client.Render.PDF(ctx, in, option.WithIdempotencyKey("inv-INV-001"))
```

## Type system

The SDK uses Go's type system to enforce the contract at compile time:

- `RenderInput` is a sealed marker interface (unexported method) satisfied by `ProjectModeInput` and `InlineModeInput` only. External types cannot satisfy it.
- `Render.PDF`, `PDFStream`, and `Document` accept `ProjectModeInput` directly — passing `InlineModeInput` is a compile-time error.
- Nullable wire fields (`ProjectID`, `Version`, `Orientation`, `Locale`, …) are `*string` to distinguish JSON `null` from empty strings.
- `Opt[T any](v T) *T` turns a literal into a `*T` for optional fields: `Version: polipage.Opt("1.0.0")`.

## Concurrency & thread-safety

`*polipage.Client` is safe for concurrent use. Create **one client per process** and reuse it across goroutines — the underlying `*http.Client` pools connections automatically. The SDK has no async/sync split; goroutines + `context.Context` cover both concurrency and cancellation.

```go
var wg sync.WaitGroup
results := make([]*polipage.DocumentDescriptor, len(invoices))
errs    := make([]error, len(invoices))
for i, inv := range invoices {
    wg.Add(1)
    go func(i int, inv Invoice) {
        defer wg.Done()
        results[i], errs[i] = client.Render.Document(ctx, polipage.ProjectModeInput{
            Project:  "billing",
            Template: "invoice",
            Version:  polipage.Opt("1.0.0"),
            Data:     inv.Data(),
        })
    }(i, inv)
}
wg.Wait()
```

## Runtime support

Server-side only. The SDK runs on:

- **Go 1.25+** — current and N-1 stable Go releases.

**Browsers / WebAssembly are not supported.** API keys (`pp_test_*`, `pp_live_*`) are secrets and must never be shipped to a browser. Call the SDK from your backend (HTTP server, CLI, worker, lambda) and proxy the result to the client.

## Requirements

Go 1.25 or later. The SDK has **zero runtime dependencies** — only the Go standard library.

## Documentation & support

- Go SDK docs: [poli-page.github.io/sdk-go/](https://poli-page.github.io/sdk-go/)
- Platform docs: [docs.poli.page](https://docs.poli.page)
- SDK API reference: [pkg.go.dev/github.com/poli-page/sdk-go](https://pkg.go.dev/github.com/poli-page/sdk-go)
- Sign up & generate API keys: [app.poli.page](https://app.poli.page)
- Issues: [github.com/poli-page/sdk-go/issues](https://github.com/poli-page/sdk-go/issues)

## License

[MIT](LICENSE) © Poli Page
