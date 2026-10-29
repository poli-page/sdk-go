# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Conformance with the Poli Page API contract v1.1.0
([poli-page/api-contract](https://github.com/poli-page/api-contract)). See
[MIGRATION.md](MIGRATION.md#from-0100-to-the-next-release-unreleased).

### Changed

- **Breaking:** `QUOTA_EXCEEDED` and `OVERAGE_CAP_EXCEEDED` (429) and
  `STORAGE_ACCESS_DENIED` (502) are no longer retried, even with a
  `Retry-After` header, and `(*Error).IsRetryable()` now returns `false` for
  them (it returned `true` for any 429 or 5xx). They last until the monthly
  reset or until the bucket access is restored, so the SDK used to wait up to
  30 s per retry for nothing. `RATE_LIMIT_EXCEEDED` and every other 429 and
  5xx are still retried. Code that requeues on `IsRetryable()` stops
  requeueing these three.
- **Breaking:** `Retry-After: 0` retries at once. It used to be treated as no
  header, so the backoff applied.
- `Retry-After` is read as integer seconds or an HTTP-date only. Any other
  value (`1.5`, `-1`, `soon`) falls back to the backoff instead of being
  parsed as a float; an integer too large for any integer type clamps to the
  30 s cap (it could wrap to a zero or negative delay). `Retry-After-Ms` is
  ignored, as before.
- SDK error messages are the exact texts of the SDK specification:
  - `invalid_options` for a missing or empty API key: `apiKey is required`
    (was `option.WithAPIKey is required`).
  - `network_error`: `Network request failed` (was the `net/http` error
    text); the `net/http` error is still the `Cause`.
  - `timeout`: `Request timed out after <ms>ms`, the attempt's timeout in
    whole milliseconds (was `request timed out after <duration>`, e.g. `1m0s`).
  - `aborted`: `Request was aborted` (was `request was aborted`).
  - `DOWNLOAD_FAILED`: `Failed to download PDF: HTTP <status>` (was
    `failed to download PDF: <status> <status text>`), and
    `Failed to download PDF: network error` when the download gets no answer
    (was the `net/http` error text, now the `Cause`).
  - A non-JSON error body: `HTTP <status>` (was
    `HTTP <status>: response body was not valid JSON`); the code is still
    `INTERNAL_ERROR` with the real status.
  - `PROJECT_REQUIRED_FOR_DOCUMENT` raised before any request:
    `project is required` (was
    `Render.Document/PDF/PDFStream require RenderInput.Project`).
- Canceling the context during the PDF download of `Render.PDF`,
  `Render.PDFStream` or `DownloadPDF` returns `aborted` instead of
  `DOWNLOAD_FAILED`, whether it happens before the storage answers or while
  the body is read.
- A 2xx API answer is read in full inside its attempt. A body cut off
  mid-read is `network_error` (`Network request failed`, the read error as
  `Cause`), and a body that outlasts the attempt's timeout is `timeout`. Both
  were `INTERNAL_ERROR` with status 200, which is not retryable; now they are
  retried like any network failure or timeout (a retried render can then
  store a second document, as with any retry after the request reached the
  API). Canceling the context while the answer is read returns `aborted`.
  `OnResponse` now fires once the body is read.
- **Breaking:** `json.Marshal` of a `Payload` (from `(*Error).ToPayload()`)
  writes `"status": null` and `"requestId": null` when they are unknown, as
  the SDK specification says; both keys used to be omitted. The Go fields
  keep their types (`Status int`, `RequestID string`, zero when unknown).
  JSON consumers that test for a missing key must test for `null`.
- An error body whose `code`, `error`, `detail`, `title` or `message` is not
  a string ignores that field instead of being read as a non-JSON body; JSON
  that is not an object gives `unknown_error` and `HTTP <status>`.

### Fixed

- A non-2xx answer whose body fails while it is read no longer takes its code
  and message from the bytes received so far, nor hides the read error. A
  context canceled during that read returns `aborted` at once, whatever the
  status: before, a 4xx gave the partial body's code (or `INTERNAL_ERROR`),
  and a 5xx or 429 fired `OnRetry` before returning `aborted`. Any other read
  failure is handled as a non-JSON error body: `INTERNAL_ERROR`,
  `HTTP <status>`, the real status (so a 5xx or 429 is still retried), and the
  read error as `Cause`.
- A failure while the PDF body is read no longer escapes as a raw `net/http`
  error: `Render.PDF`, `DownloadPDF` and reads of the `Render.PDFStream`
  stream return an `*Error`, `aborted` when the context is canceled mid-body
  and `DOWNLOAD_FAILED` with `Failed to download PDF: network error` when the
  connection drops mid-body. The read error is the `Cause`, so `errors.Is`
  (for example with `io.ErrUnexpectedEOF` or `context.Canceled`) and
  `errors.Unwrap` reach it.
- A POST (`Render.*`, `Documents.Thumbnails`) is never resent behind the
  SDK's back. `net/http`'s Transport resent it once when a reused keep-alive
  connection died before any answer, because it carries `Idempotency-Key`:
  `OnRequest` did not see that attempt, `WithMaxRetries` did not count it, and
  as the API does not deduplicate, a render could be stored twice. The failure
  now surfaces as `network_error` and goes through the SDK's own retry.
  The transport may still resend a GET once on a stale keep-alive connection,
  which stores nothing.
  The cost: over HTTP/2, `net/http` also used to resend a POST that the server
  refused before processing it (a refused stream or a `GOAWAY`). That case now
  comes back as `network_error` too and goes through the SDK's own retry, one
  backoff later; with `WithMaxRetries(0)` it reaches the caller. Such an
  attempt was never processed, so retrying it cannot duplicate a render.
- Docs: the retries page and the README no longer claim the API
  deduplicates on `Idempotency-Key`. It does not: the key is stable across the
  retries of one call, and a render retried after a late 5xx, a `timeout` or a
  `network_error` can store a second document.

### Added

- `contract_test.go` runs every SDK case of the API contract against a local
  mock server; CI pins the contract by commit (`poli-page/api-contract`
  action) and Dependabot proposes each new version. Run it locally with
  `POLI_CONTRACT=<path to contract.json> go test ./...`.

## [0.10.0] - 2026-10-27

Environments replace versions: an API key (`pp_{env}_…`) carries its
environment and a render uses the project's copy published there. Breaking,
with no deprecation window.

### Removed

- `Render.Preview`, `PreviewResult`, `InlineModeInput` and the sealed
  `RenderInput` interface: the API removed `POST /v1/render/preview` and
  inline-HTML rendering. `Documents.Preview` stays.
- The `Version` field of the render input and of `DocumentDescriptor`.
- The `Opt` helper (its only use was `Version`).
- The `Environment` type and its `EnvironmentSandbox` / `EnvironmentLive`
  constants.
- Error codes `VERSION_NOT_FOUND`, `INVALID_VERSION_FORMAT`,
  `VERSION_REQUIRED`, `INVALID_VERSION_FOR_KEY_ENV`,
  `MISSING_PROJECT_OR_TEMPLATE`, `GONE` and `PAYMENT_REQUIRED`, and the
  `ErrVersionNotFound`, `ErrGone` and `ErrPaymentRequired` sentinels.

### Changed

- `ProjectModeInput` is renamed `RenderInput` (a struct) and is the only
  render input. The request body holds only `project`, `template`, `data`,
  `metadata`, `format`, `orientation` and `locale`; unset optional fields
  are omitted.
- `DocumentDescriptor.Environment` is a plain `string` holding any
  environment name the API returns (`sandbox`, `live`, `staging`, …).
- `ThumbnailOptions` fields are all optional: an unset option is not sent
  and a zero `ThumbnailOptions` sends `{}` (API defaults: 400 px wide
  JPEG). The SDK does not check bounds.
- `Documents.Delete` documents that a re-delete succeeds (the API answers
  `204`).

### Added

- `ThumbnailFormatWebP`.
- `ErrDocumentGone` sentinel (`410 DOCUMENT_GONE`).
- Error codes `PUBLISH_IN_PROGRESS`, `ENVIRONMENT_MISMATCH`,
  `PROJECT_NOT_IN_ENVIRONMENT`, `TEMPLATE_NOT_FOUND`, `DOCUMENT_GONE`,
  `ENVIRONMENT_PROTECTED`, `ORGANIZATION_PAYMENT_GRACE`, `EMPTY_DOCUMENT`,
  `STORAGE_ACCESS_DENIED`, `ORGANIZATION_MIGRATING`, `PROJECT_NOT_FOUND`,
  `TEMPLATE_ERROR`, `THUMBNAILS_NOT_AVAILABLE`, `RATE_LIMIT_EXCEEDED`,
  `RENDER_BODY_TOO_LARGE`, `DOCUMENT_TOO_LARGE` and `RENDER_TIMEOUT`.
  `503 PUBLISH_IN_PROGRESS` is retried like any 5xx, honouring
  `Retry-After`.

### Documentation and demo

- README and docs site: the quick start renders `getting-started`/`welcome`
  with a `sandbox` key and no version. "Authentication & environments"
  has the key format, its regex, the environment table and the `sandbox`
  properties (watermarked, not counted, 30-day expiry). Template previews
  and inline HTML point to the playground, https://play.poli.page.
- `cmd/demo` asks for a `sandbox` key and checks a pasted key's format
  only. Its last step, `not-found`, renders template `does-not-exist`,
  expects `404 TEMPLATE_NOT_FOUND` and prints the error's code, status,
  message and request id; any other result fails the demo.
- The docs reference lists the new error codes per method.
- `MIGRATION.md` has a "From 0.9.x to 0.10.0" entry.

## [0.9.0-rc.1] - 2026-05-21

Release candidate for `v1.0.0`. Behaviour-identical to
`@poli-page/sdk@1.0` (see [MIGRATION.md](MIGRATION.md#10) for the parity
checklist).

### Added

- `polipage.Client` constructed via `polipage.NewClient(opts ...option.RequestOption)`.
- Functional options in the `option` subpackage:
  `WithAPIKey`, `WithBaseURL`, `WithMaxRetries`, `WithRetryDelay`,
  `WithTimeout`, `WithHTTPClient`, `WithLogger`, `WithOnRetry`,
  `WithOnError`, `WithIdempotencyKey` (per-call).
- Render namespace: `Render.PDF`, `Render.PDFStream`, `Render.Preview`,
  `Render.Document`.
- Documents namespace: `Documents.Get`, `Documents.Preview`,
  `Documents.Thumbnails`, `Documents.Delete`.
- `DocumentDescriptor.DownloadPDF(ctx)` method using the parent
  client's `*http.Client` (no auth, no retry).
- `polipage.RenderToFile(ctx, client, input, path)` free function — streams
  the PDF to disk via `Render.PDFStream` + `io.Copy`.
- Sealed `RenderInput` marker interface satisfied by `ProjectModeInput`
  and `InlineModeInput` only — `Render.PDF/PDFStream/Document` enforce
  project-mode-only at compile time.
- `Opt[T any](v T) *T` generic helper for setting optional pointer-typed
  fields with literal values.
- Typed error `*polipage.Error` with `Code`, `StatusCode`, `Message`,
  `RequestID`, `Cause`; implements `errors.Unwrap` and a custom
  `errors.Is` matcher.
- Sentinel errors: `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`,
  `ErrVersionNotFound`, `ErrDocumentNotFound`, `ErrGone`, `ErrValidation`,
  `ErrRateLimit`, `ErrTimeout`, `ErrAborted`, `ErrNetwork`,
  `ErrDownloadFailed`.
- Predicate helpers: `IsAuthError` (401 + 403), `IsRateLimitError`,
  `IsValidationError`, `IsNetworkError`, `IsRetryable`.
- Retry loop: exponential backoff with jitter `[0.5, 1.5)`,
  `Retry-After` honoured up to 30s, cancellable mid-flight via
  `context.Context.Done()`.
- `log/slog` integration via `option.WithLogger` — DEBUG/attempt,
  WARN/retry, ERROR/terminal. API keys and credentials never logged.
- Cancellation + per-call timeout via `context.Context`.
- Runnable demo at `cmd/demo` exercising every public method against
  the real API. First-run prompts for `pp_test_*` key and persists to
  `.env`; subsequent runs are silent.

### Build & supply chain

- Zero runtime dependencies — Go standard library only.
- Go 1.25 floor (CI matrix: 1.25 + 1.26 on Linux, 1.26 on macOS +
  Windows).
- golangci-lint v2 config with `bodyclose`, `errcheck`, `gocritic`,
  `gosec` (G301/G304/G306 excluded for SDK file helpers), `govet`,
  `ineffassign`, `nilerr`, `revive` (exported-doc-comments enforced),
  `staticcheck`, `unused`, plus `gofmt` and `goimports` formatters.
