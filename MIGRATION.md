# Migration Guide

This file documents breaking changes between releases of
`github.com/poli-page/sdk-go`. We follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html):
before 1.0 a breaking change bumps the minor version, from 1.0 on the
major version, and every breaking change comes with an entry here.

## From 0.10.0 to the next release (unreleased)

The SDK now follows the retry policy and the error messages of the Poli Page
API contract.

### Three codes are no longer retried

`QUOTA_EXCEEDED` and `OVERAGE_CAP_EXCEEDED` (429) and `STORAGE_ACCESS_DENIED`
(502) are returned at once, and `IsRetryable()` returns `false` for them. A
queue that requeues on `IsRetryable()` now drops these errors: handle them by
code instead, for example hold quota errors until the monthly reset and alert
on `STORAGE_ACCESS_DENIED` until the bucket role is restored.
`IsRateLimitError()` is still `true` for the two 429s, and
`errors.Is(err, polipage.ErrRateLimit)` still matches them.

```go
// 0.10.0
if pErr.IsRetryable() { // was true for QUOTA_EXCEEDED
    requeue(job)
}

// next release
switch {
case errors.Is(err, polipage.ErrRateLimit): // QUOTA_EXCEEDED or OVERAGE_CAP_EXCEEDED
    holdUntilMonthlyReset(job)
case pErr.IsRetryable():
    requeue(job)
}
```

### `Retry-After`

`Retry-After: 0` now retries at once (it used to fall back to the backoff).
Only integer seconds and HTTP-dates are read, clamped to 30 s; any other value
falls back to the backoff.

### Messages

If you match error messages, match `Code` instead: the SDK-side messages are
now the specification's exact texts (`apiKey is required`,
`Network request failed`, `Request timed out after <ms>ms`,
`Request was aborted`, `Failed to download PDF: HTTP <status>`,
`Failed to download PDF: network error`, `HTTP <status>` for a non-JSON error
body, `project is required`). The `net/http` error behind a network, timeout or
download failure is the error's `Cause`.

### `Payload` JSON

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

### Retries over HTTP/2 and mid-body failures

The SDK no longer lets `net/http` resend a POST on its own. Over HTTP/2, a
POST the server refused before processing it (refused stream, `GOAWAY`) used
to be resent silently; it now comes back as `network_error` and uses one SDK
retry. With `option.WithMaxRetries(0)` you get that `network_error`.

A 2xx answer cut off while its body is read is now `network_error` (or
`timeout`), retried by the SDK, instead of a non-retryable `INTERNAL_ERROR`
with status 200.

### Download cancellation

Canceling the context during the PDF download now returns `aborted`, not
`DOWNLOAD_FAILED`.

## From 0.9.x to 0.10.0

An API key now names its environment (`pp_{env}_…`), and a render uses
the project's copy published in that environment. The API has dropped
template versions, `POST /v1/render/preview` and inline-HTML rendering, so
this release removes them from the SDK with no deprecation window.

| 0.9.x | 0.10.0 |
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
