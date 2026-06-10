# Contributing to `github.com/poli-page/sdk-go`

Thanks for your interest. A few short rules:

## Working method

We use **TDD**: write a failing test first, then the minimum code to pass.
Each public method has a corresponding test in `*_test.go` next to the
source. See `CLAUDE.md` for the full methodology.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/):
`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`.

## Local development

```bash
go vet ./...
gofmt -l .                           # must be empty
go test -race ./...                  # unit tests
golangci-lint run ./...              # static analysis
```

Install golangci-lint v2 once:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
```

## Integration tests

Integration tests hit the API. They are gated behind a build tag and the
`POLI_PAGE_API_KEY` env var:

```bash
export POLI_PAGE_API_KEY=pp_test_...
go test -tags=integration -race -count=1 -v ./...
```

To skip integration tests on push (e.g. doc-only changes), simply run
without the `-tags=integration` flag — the default `go test` invocation
ignores them.

## Releasing

Releases are **manual**. There is no CI workflow that auto-publishes — by
design. The Go module system is its own registry: pushing a `vX.Y.Z` tag
to the repo is the entire release. `proxy.golang.org` picks it up within
minutes; `sum.golang.org` records the checksum for transparency.

1. Bump the `Version` constant in `internal/version/version.go`.
2. Move `[Unreleased]` to `[X.Y.Z] - YYYY-MM-DD` in `CHANGELOG.md`.
3. If a MAJOR bump, add a section to `MIGRATION.md`.
4. Commit `chore(release): vX.Y.Z` on `main`.
5. Run the pre-flight verification locally:
   ```bash
   go vet ./...
   gofmt -l .
   golangci-lint run ./...
   go test -race -count=1 ./...
   ```
6. Tag locally: `git tag vX.Y.Z`.
7. Push the tag when you're ready: `git push origin vX.Y.Z`.
8. Optionally create a GitHub Release page from the tag for the changelog
   excerpt — `gh release create vX.Y.Z --notes-from-tag`. There are no
   binary artifacts to attach — the module is the artifact.

### Stable vs. prerelease channels

Go modules use semver prerelease tags natively. `go get` ignores them by
default; users opt in by pinning explicitly.

#### Cutting a prerelease

1. Bump `internal/version.Version` to e.g. `2.0.0-rc.1`.
2. Move `[Unreleased]` → `[2.0.0-rc.1] - YYYY-MM-DD` in `CHANGELOG.md`.
3. Commit `chore(release): v2.0.0-rc.1`.
4. Tag and push: `git tag v2.0.0-rc.1 && git push origin v2.0.0-rc.1`.

Users opt in by pin:

```bash
go get github.com/poli-page/sdk-go@v2.0.0-rc.1
```

#### Promoting a prerelease to stable

When the prerelease is ready, cut a stable release at the same semver
minus the suffix:

1. Bump `internal/version.Version` to `2.0.0` (drop the suffix).
2. Move the prerelease entries in `CHANGELOG.md` under `[2.0.0] - YYYY-MM-DD`.
3. Commit and tag: `git tag v2.0.0 && git push origin v2.0.0`.

Stable and prerelease tags must never point at the same commit — once a
prerelease is promoted, the next prerelease starts a new pre-suffix
sequence (e.g. `2.1.0-beta.0`).
