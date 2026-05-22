# GitHub Pages doc site — design

**Date:** 2026-05-22
**Repo:** poli-page/sdk-go
**Status:** Approved (brainstorming) — awaiting implementation plan.

## Problem

GitHub Pages is enabled on the repo (`build_type: workflow`, URL
`https://poli-page.github.io/sdk-go/`) but no Pages-building workflow exists.
The Pages status is `null` — nothing has ever been deployed. The repo has a
388-line README that holds all the SDK user documentation; scrolling a single
long page is poor UX, and there is currently no navigable, searchable, themed
version of that content.

## Goal

Ship a documentation site that:

- Renders the existing README content as a multi-page, navigable, searchable
  site under `poli-page.github.io/sdk-go/`.
- Keeps `README.md` as the single source of truth (no parallel content to
  maintain).
- Publishes a versioned site (current `dev`, frozen `vX.Y.Z` per tag, mobile
  `latest` alias).
- Has zero new sources of drift: structural drift between README and site
  configuration fails the build loudly.

## Non-goals

- No new long-form guides beyond what the README already contains.
- No custom domain in this iteration (`poli-page.github.io/sdk-go/` only).
- No PR-preview deployments.
- No internationalization.
- No automated purge of old mike versions.
- No duplication of pkg.go.dev's API reference — the site links to it.

## Architecture

```
README.md  ──► cmd/docsite  ──► build/site/*.md + build/mkdocs.yml
                  (Go, testable)         │
                                         ▼
                                   mkdocs build  ──► site/ (HTML)
                                         │
                                         ▼
                              mike deploy --push   ──► gh-pages branch
                                         │
                                         ▼
                                GitHub Pages (legacy/branch mode)
```

Three components:

1. **`cmd/docsite`** — Go splitter. Reads `README.md`, writes per-page
   markdown files and the MkDocs nav.
2. **MkDocs Material + mike** — static-site generator and versioning tool.
   Config lives in `docs/mkdocs.base.yml`. Python deps pinned in
   `docs/requirements.txt`.
3. **`.github/workflows/pages.yml`** — orchestrates splitter + mike,
   chooses the version label based on the trigger, pushes to `gh-pages`.

All generated content lives under `build/` (gitignored). Sources committed to
the repo: `README.md`, `cmd/docsite/`, `docs/groups.yml`,
`docs/mkdocs.base.yml`, `docs/requirements.txt`, `.github/workflows/pages.yml`.

## Splitter (`cmd/docsite`)

### Inputs

CLI flags:

- `-readme` — path to the README (default `README.md`).
- `-groups` — path to the groups YAML (default `docs/groups.yml`).
- `-out` — output directory for generated markdown (default `build/site`).
- `-nav` — output path for the generated `mkdocs.yml` (default
  `build/mkdocs.yml`).
- `-base` — path to the MkDocs base config to merge nav into (default
  `docs/mkdocs.base.yml`).

### Splitting rules

- Everything **before** the first `## ` line becomes `index.md`. That includes
  the H1 title, badges, tagline, and the pkg.go.dev pointer.
- Each `## <Title>` starts a new page. The page contains all lines from that
  `## ` (exclusive) up to the next `## ` (exclusive).
- The leading `## <Title>` line is rewritten to `# <Title>` on the page, so
  each page has exactly one H1.
- The page filename is the kebab-case slug of the title with non-alphanumerics
  collapsed to `-` and trimmed. Examples:
  - `## Quick start` → `quick-start.md`
  - `## Error handling` → `error-handling.md`
  - `## Retries & idempotency` → `retries-idempotency.md`
- Sub-headings (`###`, `####`, …) and code blocks are preserved verbatim.

### Anchor rewriting

In-document anchor links of the form `[text](#anchor-slug)` are rewritten so
they target the correct generated page. The splitter builds a table of
`anchor-slug → page-slug` during the splitting pass. Links pointing at an
anchor inside the **same** page keep the bare `#anchor`. Links to anchors in
**different** pages are rewritten to `<page-slug>/#<anchor>` (MkDocs serves
each page as a directory).

External links (`https://…`), relative file links that are not pure anchors,
and mailto links are left untouched.

### Groups & nav generation

`docs/groups.yml` defines how H2 sections are grouped in the sidebar. It is
the only place that couples README structure to site structure.

```yaml
# docs/groups.yml
groups:
  - name: Getting started
    sections:
      - Install
      - Quick start
  - name: Concepts
    sections:
      - Working with stored documents
      - Authentication & environments
      - Methods
      - Configuration
  - name: Production
    sections:
      - Error handling
      - Cancellation
      - Observability
      - Retries & idempotency
  - name: Reference
    sections:
      - Type system
      - Concurrency
      - Runtime support
      - Requirements
  - name: Support
    sections:
      - Documentation & support
      - License
```

The splitter merges the generated nav into the base MkDocs config:

```yaml
# build/mkdocs.yml (excerpt, generated)
nav:
  - Home: index.md
  - Getting started:
    - Install: install.md
    - Quick start: quick-start.md
  - Concepts:
    - Working with stored documents: working-with-stored-documents.md
    - …
```

### Fail-loud validation

The splitter exits non-zero with a clear message in either of these cases:

- An H2 exists in the README but is not listed under any group in
  `groups.yml` (orphan H2). Message names the offending title.
- An H2 listed in `groups.yml` does not exist in the README (orphan group
  entry). Message names the offending title.

These checks prevent silent drift when a section is renamed, added, or
removed.

### Output guarantees

- The splitter is deterministic: same inputs → byte-identical output.
- Output paths are derived from input — no implicit file deletion. The
  workflow `rm -rf build/site build/mkdocs.yml` before invocation.

## MkDocs configuration

### `docs/mkdocs.base.yml`

The base config holds everything that does not depend on the README: theme,
plugins, mike provider, repo links. The splitter writes the final
`build/mkdocs.yml` by deep-merging this base with the generated `nav`.

Key choices:

- Theme: `material`.
- Features used: `navigation.tabs`, `navigation.sections`,
  `navigation.top`, `content.code.copy`, `search.suggest`.
- `extra.version.provider: mike` (renders the version selector in the
  header).
- `markdown_extensions`: `admonition`, `pymdownx.highlight`,
  `pymdownx.superfences`, `pymdownx.details`, `toc` (with permalinks).

### `docs/requirements.txt`

Pinned Python deps for the CI step. Major-version-pinned to match the CI
conventions in `CLAUDE.md` §5:

```
mkdocs-material==9.*
mike==2.*
```

## Workflow (`.github/workflows/pages.yml`)

### Triggers

- `push` on `main` — publish as `dev` (does not move `latest`).
- `push` on tag `v*.*.*` — publish as `<tag>`, move `latest` alias to it.
- `workflow_dispatch` — input field for version label, manual override.

### Permissions

- `contents: write` — mike commits to `gh-pages`.
- `pages: write`, `id-token: write` — required by GitHub Pages even in
  legacy/branch mode for some org policies; harmless if unused.

### Concurrency

```yaml
concurrency:
  group: pages
  cancel-in-progress: true
```

Prevents two mike-deploys racing on `gh-pages`.

### Job: `build-and-deploy`

Single job (Ubuntu, ~1 minute end-to-end):

1. `actions/checkout@v4` with `fetch-depth: 0` (mike needs `gh-pages`
   history).
2. `actions/setup-go@v5` reading Go version from `go.mod`.
3. `actions/setup-python@v5` (Python 3.12), pip cache.
4. `pip install -r docs/requirements.txt`.
5. `git fetch origin gh-pages --depth=1 || true` (creates the local branch
   reference if it exists; mike handles first-time creation otherwise).
6. `go run ./cmd/docsite` with default flags.
7. Choose version label based on `github.ref`:
   - Tag → `mike deploy --config-file build/mkdocs.yml --push
     --update-aliases ${TAG} latest`. Then `mike set-default latest --push`
     if it is not already the default.
   - `main` → `mike deploy --config-file build/mkdocs.yml --push dev`.
     If `mike list` shows no other versions yet, additionally run
     `mike set-default dev --push` so the root URL is not a 404 before
     the first tag.
   - Manual dispatch → `inputs.version` (required) and `inputs.alias`
     (optional, e.g. `latest`) drive the same `mike deploy` call.

mike invokes `mkdocs build` internally, so no separate build step is
needed. The implementation plan may optionally add an `mkdocs build`
sanity step before the deploy if it helps CI log diagnostics, but it is
not required.

### Pages settings switch

Pages is currently in `build_type: workflow` mode. mike requires
`build_type: legacy` (source = `gh-pages` branch). The switch is done **once
manually** before the first run via:

```
gh api -X PUT repos/poli-page/sdk-go/pages \
  -f source.branch=gh-pages -f source.path=/ -f build_type=legacy
```

This is a one-shot operation requiring repo-admin auth; not part of the
workflow. The implementation plan must call it out as a pre-merge step
done by the maintainer.

## Versioning model (mike)

| Alias / version | Source             | URL                                          |
| --------------- | ------------------ | -------------------------------------------- |
| `dev`           | every push to main | `…/sdk-go/dev/`                              |
| `vX.Y.Z`        | every `v*` tag     | `…/sdk-go/vX.Y.Z/`                           |
| `latest`        | most recent tag    | `…/sdk-go/latest/` (also default at root)    |

- Before the first tag exists, the default is `dev`, so the root URL is
  never a 404.
- Deletions are manual: `mike delete <version> --push` when a version is
  retired. Not automated in this design.

## Testing strategy

### Splitter — unit tests (`cmd/docsite/main_test.go`)

TDD, one failing test at a time per `CLAUDE.md` §3. Fixtures under
`cmd/docsite/testdata/`.

| Test                         | Asserts                                                                                  |
| ---------------------------- | ---------------------------------------------------------------------------------------- |
| `TestSplit_Preamble`         | Content before the first `##` lands in `index.md`.                                       |
| `TestSplit_OneFilePerH2`     | `N` H2 sections produce `N` per-section files plus `index.md`.                           |
| `TestSplit_SlugKebabCase`    | `## Error handling` → `error-handling.md`; `## Retries & idempotency` strips `&`.        |
| `TestSplit_H2DemotedToH1`    | The `## Title` line becomes `# Title` on the generated page.                             |
| `TestSplit_PreservesH3AndBelow` | `###`, `####`, code fences, and inline content are byte-identical.                    |
| `TestSplit_AnchorRewriting`  | `[x](#error-handling)` → `[x](error-handling/)`; external links untouched.               |
| `TestSplit_NavGeneration`    | Generated nav matches the order/grouping in `groups.yml`.                                |
| `TestSplit_OrphanH2_FailsLoud` | README H2 missing from `groups.yml` → exit non-zero, message names the title.          |
| `TestSplit_OrphanGroup_FailsLoud` | `groups.yml` entry missing from README → exit non-zero, message names the title.    |
| `TestSplit_RealReadme`       | End-to-end run on the live `README.md`: expected page count, nav structure, no errors.   |

### Workflow

Not testable locally in a clean way. First real run is on CI in the PR that
introduces it. Acceptance: `mike list` on `gh-pages` shows `dev`, root URL
resolves, page navigation works in a browser.

### Lint / format

`gofmt`, `go vet`, `golangci-lint` already enforced by `ci.yml`. Splitter
code follows the same standards. No new tooling.

## Repo layout after this work

```
cmd/
  demo/                     (existing)
  docsite/
    main.go                 ← splitter entry point
    split.go                ← splitting + slug + anchor logic
    nav.go                  ← nav merging
    main_test.go            ← unit tests
    testdata/
      simple/README.md
      simple/groups.yml
      simple/expected/...
docs/
  groups.yml                ← H2 → group mapping
  mkdocs.base.yml           ← MkDocs Material + mike config
  requirements.txt          ← mkdocs-material + mike, pinned
  superpowers/
    specs/
      2026-05-22-gh-pages-doc-site-design.md  ← this file
.github/
  workflows/
    pages.yml               ← the new workflow
.gitignore                  ← + /build/
```

(File splitting inside `cmd/docsite` — `split.go` / `nav.go` — is suggested
for clarity; the implementation plan can collapse to a single `main.go` if
the splitter stays small enough.)

## Open follow-ups (deliberately out of scope)

- Custom domain (`go.poli.page` or `sdk-go.poli.page`) with CNAME.
- PR-preview deployments via artifact upload without mike push.
- Automated purge of old mike versions.
- i18n.
- Pulling `example_test.go` content into the site as runnable examples.

## Acceptance criteria

- `https://poli-page.github.io/sdk-go/` returns 200 and renders a navigable
  site.
- Pushing to `main` updates `…/sdk-go/dev/` within ~1 minute.
- Tagging `v0.x.y` publishes `…/sdk-go/v0.x.y/` and updates `…/latest/`.
- Renaming an H2 in `README.md` without updating `docs/groups.yml` fails
  CI with a clear message.
- `go test ./cmd/docsite/...` passes locally and in CI.
- No new dependencies in `go.mod`. Python deps live only in
  `docs/requirements.txt`.
