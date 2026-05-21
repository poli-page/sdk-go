# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
