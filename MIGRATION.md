# Migration Guide

This file documents breaking changes between major versions of
`github.com/poli-page/sdk-go`. We follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html):
breaking changes only ship in major version bumps and always come with
an entry here.

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

// Render namespace
//   client.Render.PDF, PDFStream, Document → project mode only
//                                            (Project + Template + Version)
//   client.Render.Preview                   → both project and inline mode

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
same hooks-never-break-the-request semantics, same project-mode-only
constraint on `Render.PDF/PDFStream/Document`, same primitive-only
`RenderMetadata`, same thumbnails wire wrap/unwrap, same
`Documents.Preview` text/html + `X-Document-Page-Count` parsing.
