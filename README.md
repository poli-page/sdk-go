# sdk-go (Go SDK)

Official Go SDK for [Poli Page](https://poli.page) — generate PDFs from any Go HTTP server, CLI, or worker.

> **Status**: scaffold only. Implementation begins in P3.0 of the [SDK roadmap](https://github.com/poli-page/poli-page/blob/develop/docs/onboarding/micka/sdk-roadmap.md).

## Install

```bash
go get github.com/poli-page/sdk-go
```

## Quick start

To be filled in as the SDK is built. The client will follow the contract defined in `docs/onboarding/micka/sdk-specification.md` of the platform repo and mirror the Node.js reference implementation.

## Recipes

Per Go convention there is no separate "framework integration" package — idiomatic Go libraries expose `http.Handler`-shaped APIs that net/http (stdlib), Gin, Echo, Fiber, and Chi all consume identically. Recipes for each of these live under `examples/` once implemented.

## Publishing

Published as a **Go module**: [`github.com/poli-page/sdk-go`](https://github.com/poli-page/sdk-go). Versioning follows Go module semantic-import rules (`/v2`, `/v3` import paths once we cross the `1.0.0` mark and break the API).

## Documentation

Full Poli Page documentation is at [docs.poli.page](https://docs.poli.page).

## License

MIT — see [LICENSE](./LICENSE).
