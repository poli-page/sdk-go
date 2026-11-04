# Migration Guide

This file documents breaking changes between releases of
`github.com/poli-page/sdk-go`. We follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html):
before 1.0 a breaking change bumps the minor version, from 1.0 on the
major version, and every breaking change comes with an entry here.

## From 0.11.0 to 0.13.0

0.12 was never released for Go: the SDK now shares its major.minor with the
Poli Page API, and 0.13.0 passes the API contract v0.13.0. The module path
does not change.

| 0.11.0 | 0.13.0 |
| --- | --- |
| `ErrCodeIOFailed` (`io_failed`), `ErrIOFailed` | `ErrCodeIOError` (`io_error`), `ErrIOError` |
| `RenderToFile` write failures: `failed to create directory: …`, `failed to create file: …`, `failed to write file: …` | `Failed to write PDF: <path>: <reason>`, e.g. `Failed to write PDF: /out/invoice.pdf: permission denied` |
| `RenderToFile` on a read-only target: fails after the render | `io_error` before any API call |
| `RenderToFile` to a FIFO, socket or device (`/dev/stdout`): written through | `io_error`, `not a regular file`, before any API call: copy `Render.PDFStream` into it instead |
| `option.WithTimeout(0)`: every attempt timed out at once | no SDK bound: only the caller's context bounds the call |
| An API attempt under a ctx with a deadline: the deadline replaced `option.WithTimeout` | the shorter of the SDK timeout and the caller's deadline wins |
| A 2xx whose body is not JSON: `INTERNAL_ERROR`, `failed to decode response body: …` | `invalid_response` (`ErrCodeInvalidResponse`, `ErrInvalidResponse`), `Invalid response: HTTP <status> body is not JSON` |
| A download past its timeout: `Failed to download PDF: network error` | `Failed to download PDF: timeout` (still `DOWNLOAD_FAILED`, no status) |
| `Render.PDF`, `Render.PDFStream`, `RenderToFile` downloads: no SDK timeout | bounded by `option.WithTimeout` / `option.WithRequestTimeout` |
| `DownloadPDF`: no SDK timeout without `option.WithDownloadTimeout` | bounded by `option.WithTimeout`; `option.WithDownloadTimeout(0)` removes the bound |

### `io_failed` is now `io_error`

```go
// 0.11.0
if errors.Is(err, polipage.ErrIOFailed) { … }

// 0.13.0
if errors.Is(err, polipage.ErrIOError) { … }
```

The `os` error is still the `Cause`, so `errors.As(err, &pathErr)` with an
`*os.PathError` works as before. A failed `RenderToFile` no longer leaves a
partial file: the target keeps its previous content, or is not created. A
symbolic link is written through and stays a link; an existing target keeps
its permission bits (without setuid, setgid or sticky), not its owner or
group, and other hard links to it keep the old content. A read-only target is
refused before any API call, and so is a target that is not a regular file:

```go
// 0.11.0
err := polipage.RenderToFile(ctx, client, in, "/dev/stdout")

// 0.13.0
body, err := client.Render.PDFStream(ctx, in)
if err != nil {
    return err
}
defer body.Close()
_, err = io.Copy(os.Stdout, body)
```

### A non-JSON 2xx is `invalid_response`

A proxy or captive portal that answers `200` with an HTML page used to give
`INTERNAL_ERROR`. It now gives `invalid_response`, with the real status in
`StatusCode` and `502` in `ToPayload().Status`. It is never retried, as
before. Code that matched `INTERNAL_ERROR` with a 2xx `StatusCode` should
match `polipage.ErrInvalidResponse` instead.

### The PDF download has a timeout

The download now stops after the client's timeout (60 s by default). For a
very large PDF fetched with `Render.PDF` or `DownloadPDF` on a slow link,
raise `option.WithTimeout`, pass `option.WithDownloadTimeout` to
`DownloadPDF`, or use `Render.PDFStream`, whose timeout applies to each
read rather than to the whole download. A context with its own deadline
no longer replaces the SDK's timeout for the download: the first bound to
end wins.

### The SDK timeout and a context deadline compose

The same holds for each API attempt: the shorter of the SDK timeout
(`option.WithTimeout`, `option.WithRequestTimeout`) and the caller's
deadline wins. Before, a context with a deadline replaced the SDK timeout,
so one stalled attempt could use all of it. When the SDK timeout fires, the
error is `timeout` with `Request timed out after <ms>ms` and is retried; when
the caller's deadline fires, it is `timeout` too but is not retried, as
before.

```go
// 0.13.0: each attempt stops after 10 s, the whole call after 2 min.
client := polipage.NewClient(option.WithAPIKey(key), option.WithTimeout(10*time.Second))
ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
defer cancel()
doc, err := client.Render.Document(ctx, in)
```

## From 0.9.x to 0.11.0

0.10.0 was never released for Go: 0.11.0 follows 0.9.0 and carries both groups
of changes below.

### Environments replace versions

An API key now names its environment (`pp_{env}_…`), and a render uses
the project's copy published in that environment. The API has dropped
template versions, `POST /v1/render/preview` and inline-HTML rendering, so
this release removes them from the SDK with no deprecation window.

| 0.9.x | 0.11.0 |
| --- | --- |
| `polipage.ProjectModeInput{…, Version: polipage.Opt("1.0.0")}` | `polipage.RenderInput{…}`: publish the project to the key's environment instead of pinning a version |
| `polipage.Opt` | removed (its only use was `Version`) |
| `client.Render.Preview(ctx, polipage.InlineModeInput{…})`, `PreviewResult` | removed: try templates and raw HTML in the playground, https://play.poli.page. `client.Documents.Preview(ctx, id)` stays |
| `RenderInput` (sealed interface) | `RenderInput` is now the struct formerly named `ProjectModeInput` |
| `DocumentDescriptor.Version` | removed |
| `DocumentDescriptor.Environment` of type `polipage.Environment`, `EnvironmentSandbox`, `EnvironmentLive` | a plain `string` with any environment name: compare with `"sandbox"`, `"live"`, … |
| `pp_test_…` keys | `pp_sandbox_…` keys. Service keys use the same `pp_{env}_…` format (no more `pp_sa_…`) |
| `ErrVersionNotFound`, `VERSION_NOT_FOUND`, `INVALID_VERSION_FORMAT`, `VERSION_REQUIRED`, `INVALID_VERSION_FOR_KEY_ENV` | removed: a render fails with `PROJECT_NOT_FOUND`, `PROJECT_NOT_IN_ENVIRONMENT` or `TEMPLATE_NOT_FOUND` |
| `ErrGone`, `ErrCodeGone` (`GONE`) | `ErrDocumentGone`, `ErrCodeDocumentGone` (`DOCUMENT_GONE`) |
| `ErrPaymentRequired`, `ErrCodePaymentRequired` (`PAYMENT_REQUIRED`) | removed: past-due billing is reported as `ORGANIZATION_PAYMENT_GRACE` |
| `MISSING_PROJECT_OR_TEMPLATE` | removed |
| `ThumbnailOptions.Width` always sent | every field optional: a zero value is not sent (API defaults: 400 px, `jpeg`) |

See [CHANGELOG.md](CHANGELOG.md) for the full list,
including the new error codes.

### API contract v1.3.0

The SDK now follows the retry policy and the error messages of the Poli Page
API contract.

#### Three codes are no longer retried

`QUOTA_EXCEEDED` and `OVERAGE_CAP_EXCEEDED` (429) and `STORAGE_ACCESS_DENIED`
(502) are returned at once, and `IsRetryable()` returns `false` for them. A
queue that requeues on `IsRetryable()` now drops these errors: handle them by
code instead, for example hold quota errors until the monthly reset and alert
on `STORAGE_ACCESS_DENIED` until the bucket role is restored.
`IsRateLimitError()` is still `true` for the two 429s, and
`errors.Is(err, polipage.ErrRateLimit)` still matches them.

```go
// 0.9.x
if pErr.IsRetryable() { // was true for QUOTA_EXCEEDED
    requeue(job)
}

// 0.11.0
switch {
case errors.Is(err, polipage.ErrRateLimit): // QUOTA_EXCEEDED or OVERAGE_CAP_EXCEEDED
    holdUntilMonthlyReset(job)
case pErr.IsRetryable():
    requeue(job)
}
```

#### `DOWNLOAD_FAILED` is never retryable

`IsRetryable()` returns `false` for a `DOWNLOAD_FAILED` whatever the storage
status; it was `true` for a storage 5xx or 429. A queue that requeues on
`IsRetryable()` now drops it at once: call `Documents.Get` for a fresh
presigned URL and download again.

#### `Retry-After`

`Retry-After: 0` now retries at once (it used to fall back to the backoff).
Only integer seconds and HTTP-dates are read, clamped to 30 s; any other value
falls back to the backoff.

#### Messages

If you match error messages, match `Code` instead: the SDK-side messages are
now the specification's exact texts (`apiKey is required`,
`Network request failed`, `Request timed out after <ms>ms`,
`Request was aborted`, `Failed to download PDF: HTTP <status>`,
`Failed to download PDF: network error`, `HTTP <status>` for a non-JSON error
body, `project is required`). The `net/http` error behind a network, timeout or
download failure is the error's `Cause`.

#### `Payload` JSON

`json.Marshal(err.ToPayload())` now writes `"status": null` and
`"requestId": null` when they are unknown, instead of leaving the keys out.
The Go fields are unchanged.

`Payload` now has its own `MarshalJSON`, and Go promotes it to any struct
that embeds `Payload`: such a struct marshals as the `Payload` alone, and its
other fields drop out of the JSON. Use a named field instead:

```go
// drops Path from the JSON
type errorResponse struct {
    polipage.Payload
    Path string `json:"path"`
}

// keeps both
type errorResponse struct {
    Error polipage.Payload `json:"error"`
    Path  string           `json:"path"`
}
```

#### Retries over HTTP/2 and mid-body failures

The SDK no longer lets `net/http` resend a POST on its own. Over HTTP/2, a
POST the server refused before processing it (refused stream, `GOAWAY`) used
to be resent silently; it now comes back as `network_error` and uses one SDK
retry. With `option.WithMaxRetries(0)` you get that `network_error`.

A 2xx answer cut off while its body is read is now `network_error` (or
`timeout`), retried by the SDK, instead of a non-retryable `INTERNAL_ERROR`
with status 200.

#### Download cancellation

Canceling the context during the PDF download now returns `aborted`, not
`DOWNLOAD_FAILED`.

## 1.0

The first stable release. Treat `v1.0.0` as the starting point for the
Go SDK — there is no prior published surface to migrate from.

### Surface

```go
import (
    polipage "github.com/poli-page/sdk-go"
    "github.com/poli-page/sdk-go/option"
)

client := polipage.NewClient(option.WithAPIKey(apiKey))

// Render namespace (polipage.RenderInput: Project, Template, Data required;
// Metadata, Format, Orientation, Locale optional, omitted when unset;
// the project's copy published in the API key's environment is rendered)
//   client.Render.PDF, PDFStream, Document

// Documents namespace (stored-document lifecycle)
//   client.Documents.Get, Preview, Thumbnails, Delete

// File helper (free function)
//   polipage.RenderToFile(ctx, client, input, path)
```

### Behaviour parity with `@poli-page/sdk@1.0`

`v1.0.0` of the Go SDK is behaviour-identical to `@poli-page/sdk@1.0` —
same retry policy (5xx + 429 + network + timeout; jitter `[0.5, 1.5)`;
Retry-After cap 30s), same error-code round-tripping, same predicate
helpers (`IsAuthError` covers 401 + 403), same constructor validation,
same hooks-never-break-the-request semantics, same primitive-only
`RenderMetadata`, same thumbnails wire wrap/unwrap, same
`Documents.Preview` text/html + `X-Document-Page-Count` parsing.
