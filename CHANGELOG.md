# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.10.0] - 2026-10-26

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
  only. Its last step, `not-found`, renders template `does-not-exist` and
  prints the error's code, status, message and request id
  (`404 TEMPLATE_NOT_FOUND`).
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
