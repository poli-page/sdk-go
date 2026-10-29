# Poli Page SDK for Go

[![Release](https://img.shields.io/github/v/release/poli-page/sdk-go?display_name=tag&sort=semver&style=flat&logo=go&logoColor=ffffff&label=Release)](https://github.com/poli-page/sdk-go/releases)
[![Ci](https://img.shields.io/github/actions/workflow/status/poli-page/sdk-go/ci.yml?branch=main&style=flat&logo=githubactions&logoColor=ffffff&label=Ci)](https://github.com/poli-page/sdk-go/actions/workflows/ci.yml)
[![Codeql](https://img.shields.io/github/actions/workflow/status/poli-page/sdk-go/codeql.yml?branch=main&style=flat&logo=github&logoColor=ffffff&label=Codeql)](https://github.com/poli-page/sdk-go/actions/workflows/codeql.yml)
[![Coverage](https://img.shields.io/codecov/c/github/poli-page/sdk-go?style=flat&logo=codecov&logoColor=ffffff&label=Coverage)](https://codecov.io/gh/poli-page/sdk-go)
[![Go](https://img.shields.io/badge/Go-1.25%2B-blue?style=flat&logo=go&logoColor=ffffff)](https://go.dev/)
[![Lint](https://img.shields.io/badge/Lint-golangci--lint-blue?style=flat&logo=go&logoColor=ffffff)](https://golangci-lint.run/)
[![Go.dev](https://img.shields.io/badge/Go.dev-reference-blue?style=flat&logo=go&logoColor=ffffff)](https://pkg.go.dev/github.com/poli-page/sdk-go)
[![Report](https://img.shields.io/badge/Report-A%2B-blue?style=flat&logo=go&logoColor=ffffff)](https://goreportcard.com/report/github.com/poli-page/sdk-go)
[![Deps](https://img.shields.io/badge/Deps-up%20to%20date-brightgreen?style=flat&logo=go&logoColor=ffffff)](https://github.com/poli-page/sdk-go/network/dependencies)
[![Docs](https://img.shields.io/badge/Docs-online-brightgreen?style=flat&logo=readthedocs&logoColor=ffffff)](https://poli-page.github.io/sdk-go/)
[![License](https://img.shields.io/github/license/poli-page/sdk-go?style=flat&logo=gnu&logoColor=ffffff&label=License)](LICENSE)

Official Go SDK for [Poli Page](https://poli.page) — render polished PDFs from HTML templates via the Poli Page API.

→ Documentation: **<https://poli-page.github.io/sdk-go/>**

→ API reference (auto-generated from doc comments): **[pkg.go.dev/github.com/poli-page/sdk-go](https://pkg.go.dev/github.com/poli-page/sdk-go)**

## Install

```bash
go get github.com/poli-page/sdk-go
```

Requires Go 1.25 or later. Zero runtime dependencies.

## Quick start

### Render the welcome template

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

    pdf, err := client.Render.PDF(context.Background(), polipage.RenderInput{
        Project:  "getting-started",
        Template: "welcome",
        Data:     map[string]any{"name": "SDK Demo"},
    })
    if err != nil {
        log.Fatal(err)
    }
    if err := os.WriteFile("welcome.pdf", pdf, 0o644); err != nil {
        log.Fatal(err)
    }
}
```

Run it with a sandbox key: `POLI_PAGE_API_KEY=pp_sandbox_x go run .` (replace `pp_sandbox_x` with your key). Every organization gets a `getting-started` project with a `welcome` template, published into its `sandbox` environment when the organization is created, so the snippet runs as-is on a fresh organization. `getting-started` is a normal, editable project: if your organization renamed it, deleted it or removed it from `sandbox`, the API answers `404 PROJECT_NOT_FOUND` or `404 PROJECT_NOT_IN_ENVIRONMENT`. Use a fresh organization in that case.

A render uses the project's copy published in the API key's environment. For your own templates, publish the project to that environment, then swap the slugs:

```go
pdf, err := client.Render.PDF(ctx, polipage.RenderInput{
    Project:  "billing",
    Template: "invoice",
    Data:     map[string]any{"invoiceNumber": "INV-001", "total": 1280},
})
```

To try a template or raw HTML without an API key, use the playground at [play.poli.page](https://play.poli.page).

### Write a PDF to disk

```go
err := polipage.RenderToFile(ctx, client, polipage.RenderInput{
    Project:  "getting-started",
    Template: "welcome",
    Data:     map[string]any{"name": "SDK Demo"},
}, "./welcome.pdf")
```

`RenderToFile` streams response bytes directly to disk (bounded memory). Parent directories are created on demand; existing files are overwritten.

### Try it locally — runnable demo

The repo ships a runnable demo that exercises every public method end-to-end against the API:

```bash
go run ./cmd/demo
# or remote-only, no checkout:
go run github.com/poli-page/sdk-go/cmd/demo@latest
```

The demo reads `POLI_PAGE_API_KEY` and the optional `POLI_PAGE_BASE_URL` from the shell or `./.env`. With no key, it asks for a sandbox key (`pp_sandbox_…`), checks its format and saves it to `.env`, so later runs are silent. It renders `getting-started`/`welcome` and writes its output to `./output/`, in nine steps:

1. `Render.PDF`
2. `Render.PDFStream`
3. `RenderToFile`
4. `Render.Document`
5. `Documents.Get`
6. `Documents.Thumbnails` (the demo carries on if the plan does not include thumbnails)
7. `Documents.Preview`
8. `Documents.Delete`
9. `not-found`: renders template `does-not-exist` with empty data. The API answers `404 TEMPLATE_NOT_FOUND`, and the demo prints the error's code, status, message and request id. Any other result (a success, a network error, another API error) fails the demo.

### Stream — for large PDFs or piping to S3 / HTTP responses

```go
body, err := client.Render.PDFStream(ctx, polipage.RenderInput{
    Project:  "billing",
    Template: "invoice",
    Data:     map[string]any{"invoiceNumber": "INV-001"},
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
doc, err := client.Render.Document(ctx, polipage.RenderInput{
    Project:  "billing",
    Template: "invoice",
    Data:     map[string]any{"invoiceNumber": "INV-001"},
    Metadata: polipage.RenderMetadata{"customerId": "cust_123"}, // your own audit data
})
// doc.DocumentID, doc.Environment, doc.PageCount, doc.SizeBytes, doc.PresignedPDFURL, doc.Metadata, ...

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

// 5. When done, soft-delete (deleting it again also succeeds)
err = client.Documents.Delete(ctx, doc.DocumentID)
```

The presigned URL has a ~15-minute TTL. If `DownloadPDF` fails with `Code: ErrCodeDownloadFailed` (HTTP 403 from S3), call `Documents.Get(ctx, id)` to refresh and retry.

## Authentication & environments

An API key belongs to one environment of your organization, and its text names that environment: `pp_{env}_` followed by 32 lowercase hex characters. A full key matches:

```text
^pp_(?=[a-z][a-z0-9-]{0,19}_)[a-z][a-z0-9]*(?:-[a-z0-9]+)*_[0-9a-f]{32}$
```

| Environment                           | Key            | Watermark | Counted |
| ------------------------------------- | -------------- | --------- | ------- |
| `sandbox` (every org)                 | `pp_sandbox_…` | yes       | no      |
| `live` (every org, deletable on Pro+) | `pp_live_…`    | no        | yes     |
| created on Pro+ (e.g. `staging`)      | `pp_staging_…` | no        | yes     |

"Counted" means the render counts toward the organization's monthly PDF quota. `sandbox` documents are watermarked, are not counted and expire 30 days after creation. There is no watermark option: a template that wants a watermark outside `sandbox` uses the `poli-watermark` class.

A render uses the project's copy published in the key's environment, and `DocumentDescriptor.Environment` reports the environment a document was rendered in. Every key uses the same endpoint, `https://api.poli.page`. The SDK sends the key as a Bearer token and never inspects or validates it.

## Methods

| Method                                                | Returns                              | Description |
| ----------------------------------------------------- | ------------------------------------ | ----------- |
| `client.Render.PDF(ctx, in, opts...)`                 | `([]byte, error)`                    | Render a PDF, return bytes |
| `client.Render.PDFStream(ctx, in, opts...)`           | `(io.ReadCloser, error)`             | Render and stream the response |
| `client.Render.Document(ctx, in, opts...)`            | `(*DocumentDescriptor, error)`       | Render and return descriptor (skip auto-download) |
| `client.Documents.Get(ctx, id, opts...)`              | `(*DocumentDescriptor, error)`       | Retrieve a stored document |
| `client.Documents.Preview(ctx, id, opts...)`          | `(*DocumentPreviewResult, error)`    | Stored document's paginated HTML |
| `client.Documents.Thumbnails(ctx, id, options, opts...)` | `([]Thumbnail, error)`            | Page thumbnails (JPEG/WebP/PNG, base64) |
| `client.Documents.Delete(ctx, id, opts...)`           | `error`                              | Soft-delete a stored document (a re-delete also succeeds) |
| `(*DocumentDescriptor).DownloadPDF(ctx)`              | `([]byte, error)`                    | Fetch bytes from the descriptor's presigned URL |
| `polipage.RenderToFile(ctx, client, in, path)`        | `error`                              | Render and stream to disk |

## Configuration

All options live in the [`option`](https://pkg.go.dev/github.com/poli-page/sdk-go/option) subpackage and are passed to `NewClient` (variadic). Per-call options (currently only `WithIdempotencyKey`) are passed to individual methods as the final variadic argument.

| Option                                | Type                          | Default                  | Description |
| ------------------------------------- | ----------------------------- | ------------------------ | ----------- |
| `option.WithAPIKey(key)`               | `string`                      | (required)              | API key; it names its environment (`pp_{env}_…`) |
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
    if errors.Is(err, polipage.ErrUnauthorized) { return refreshCredentials() } // MISSING_API_KEY or INVALID_API_KEY

    var pErr *polipage.Error
    if errors.As(err, &pErr) {
        if pErr.IsRateLimitError()  { return queueForLater() } // any 429: quota codes are never retried
        if pErr.IsValidationError() { log.Println("bad input:", pErr.Message) }
        if pErr.IsNetworkError()    { log.Println("network/timeout") }
        log.Printf("code=%s status=%d requestId=%s\n", pErr.Code, pErr.StatusCode, pErr.RequestID)
    }
    return err
}
```

A sentinel matches by error code, not by HTTP status. `ErrRateLimit` matches `QUOTA_EXCEEDED` and `OVERAGE_CAP_EXCEEDED`; to catch every `429`, use `IsRateLimitError()` as above. `ErrNotFound` matches only the generic `NOT_FOUND` code, so check the specific codes of a failed render:

```go
var pErr *polipage.Error
if errors.As(err, &pErr) {
    switch pErr.Code {
    case polipage.ErrCodeProjectNotFound:
        return showBanner("No project matches this slug: check it, or whether the project was renamed or deleted.")
    case polipage.ErrCodeProjectNotInEnvironment:
        return showBanner("The project is not published in this key's environment.")
    case polipage.ErrCodeTemplateNotFound:
        return show404()
    case polipage.ErrCodeOrganizationPaymentGrace:
        return showBanner("Billing is past due.")
    case polipage.ErrCodeOrganizationCancelled:
        return showBanner("Subscription cancelled — service is read-only.")
    case polipage.ErrCodeDocumentGone:
        return show410() // the document was deleted or has expired
    }
}
```

Unknown codes from a newer API pass through verbatim in `pErr.Code`.

## Cancellation

Pass a `context.Context` with a deadline or `cancel` to abort a render in flight:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

pdf, err := client.Render.PDF(ctx, in)
```

When the context is canceled, the SDK returns `*polipage.Error` with `Code == ErrCodeAborted`, also while the PDF is downloaded or streamed; when the deadline expires, `Code == ErrCodeTimeout`. Caller-aborted errors are never retried.

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

The SDK retries on **5xx**, **429**, **network errors**, and **timeouts**, except `QUOTA_EXCEEDED`, `OVERAGE_CAP_EXCEEDED` (429) and `STORAGE_ACCESS_DENIED` (502), which waiting cannot fix: those are returned at once. Before retry `n` (1-based) the SDK waits `RetryDelay × 2^(n−1) × j`, with `j` a random factor in `[0.5, 1.5)`; a `Retry-After` header (integer seconds or HTTP-date, clamped to 30 s) replaces it when the server sends one. `Retry-After-Ms` is ignored. Go's HTTP transport may resend a GET once on a stale keep-alive connection; it never resends a POST. Over HTTP/2, a POST the server refuses before processing it (refused stream, `GOAWAY`) comes back as `network_error` and goes through the SDK's own retry; with `WithMaxRetries(0)` it is returned to you. Every POST sends an auto-generated `Idempotency-Key` (UUID v4), the same on every retry of that call; the API does not deduplicate on it, so a retried render can store a second document. Pass `option.WithIdempotencyKey("…")` as the last argument to override:

```go
_, err := client.Render.PDF(ctx, in, option.WithIdempotencyKey("inv-INV-001"))
```

## Type system

- `RenderInput` is a struct and the only input of `Render.PDF`, `PDFStream`, `Document` and `RenderToFile`. `Project`, `Template` and `Data` are required; `Format`, `Orientation`, `Locale` and `Metadata` are optional and left out of the request when unset. Pass a non-nil `Data` (an empty `map[string]any{}` is sent as `{}`).
- `PageFormat`, `Orientation` and `ThumbnailFormat` are string types with named constants (`PageFormatA4`, `OrientationLandscape`, `ThumbnailFormatWebP`, …).
- Nullable wire fields of `DocumentDescriptor` (`ProjectID`, `TemplateSlug`, `Orientation`, `Locale`, …) are `*string` to distinguish JSON `null` from empty strings. `Environment` is a plain `string`: any environment name the API returns.

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
        results[i], errs[i] = client.Render.Document(ctx, polipage.RenderInput{
            Project:  "billing",
            Template: "invoice",
            Data:     inv.Data(),
        })
    }(i, inv)
}
wg.Wait()
```

## Runtime support

Server-side only. The SDK runs on:

- **Go 1.25+** — current and N-1 stable Go releases.

**Browsers / WebAssembly are not supported.** API keys are secrets, whatever their environment, and must never be shipped to a browser. Call the SDK from your backend (HTTP server, CLI, worker, lambda) and proxy the result to the client.

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
