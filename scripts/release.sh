#!/usr/bin/env bash
# release.sh — local pre-flight + tag for a Poli Page SDK Go release.
#
# Usage:
#   scripts/release.sh vX.Y.Z              # full release: verify + confirm + tag locally
#   scripts/release.sh --dry-run vX.Y.Z    # everything except the tag
#
# Pushing the tag is a separate manual step (`git push origin vX.Y.Z`) so
# the release moment stays explicit. There is no CI workflow that auto-
# publishes — proxy.golang.org indexes the tag once it lands on the remote;
# that is the entire publishing surface (matches anthropic-sdk-go,
# openai-go, stripe-go).

set -euo pipefail

# ─── argument parsing ───────────────────────────────────────────────────

dry_run=0
version=""
for arg in "$@"; do
  case "$arg" in
    --dry-run) dry_run=1 ;;
    --help|-h)
      sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    v*) version="$arg" ;;
    *)  echo "unknown arg: $arg" >&2; exit 64 ;;
  esac
done

if [[ -z "$version" ]]; then
  echo "usage: $0 [--dry-run] vX.Y.Z" >&2
  exit 64
fi

if ! [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "✗ version must be vMAJOR.MINOR.PATCH[-pre], got $version" >&2
  exit 64
fi

# ─── colors ─────────────────────────────────────────────────────────────

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  bold=$'\033[1m'; dim=$'\033[2m'; red=$'\033[31m'; green=$'\033[32m'
  yellow=$'\033[33m'; cyan=$'\033[36m'; reset=$'\033[0m'
else
  bold=; dim=; red=; green=; yellow=; cyan=; reset=
fi

step()  { echo; echo "${cyan}${bold}▶ $*${reset}"; }
ok()    { echo "  ${green}✔${reset} $*"; }
warn()  { echo "  ${yellow}⚠${reset} $*"; }
fail()  { echo "  ${red}✗${reset} $*" >&2; exit 1; }

# ─── pre-flight ─────────────────────────────────────────────────────────

step "Pre-flight"

branch=$(git rev-parse --abbrev-ref HEAD)
if [[ "$branch" != "main" ]]; then
  fail "must be on main; currently on $branch"
fi
ok "on main"

if ! git diff --quiet || ! git diff --cached --quiet; then
  fail "working tree is dirty; commit or stash before releasing"
fi
ok "working tree clean"

if git rev-parse "$version" >/dev/null 2>&1; then
  fail "tag $version already exists locally"
fi
if git ls-remote --tags origin "refs/tags/$version" | grep -q .; then
  fail "tag $version already exists on origin"
fi
ok "tag $version is fresh"

# Cross-check internal/version/version.go matches the release version
# (strip leading v).
declared=$(grep -oE '"[^"]*"' internal/version/version.go | tr -d '"')
expected="${version#v}"
if [[ "$declared" != "$expected" ]]; then
  fail "internal/version.Version = \"$declared\", expected \"$expected\" — bump it before releasing"
fi
ok "internal/version.Version = $declared"

# ─── verify ─────────────────────────────────────────────────────────────

step "go vet"
go vet ./...
ok "vet clean"

step "gofmt -l ."
unformatted=$(gofmt -l .)
if [[ -n "$unformatted" ]]; then
  echo "$unformatted"
  fail "files are not gofmt-clean"
fi
ok "gofmt clean"

step "golangci-lint run ./..."
if ! command -v golangci-lint >/dev/null 2>&1; then
  warn "golangci-lint not on PATH; installing v2.12.2"
  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
fi
"$(go env GOPATH)/bin/golangci-lint" run ./... || command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./...
ok "lint clean"

step "govulncheck ./..."
if ! command -v govulncheck >/dev/null 2>&1; then
  warn "govulncheck not on PATH; installing latest"
  go install golang.org/x/vuln/cmd/govulncheck@latest
fi
"$(go env GOPATH)/bin/govulncheck" ./... || govulncheck ./...
ok "no known vulns"

step "go test -race -count=1 ./..."
go test -race -count=1 ./...
ok "all unit tests pass"

if [[ -n "${POLI_PAGE_API_KEY:-}" ]]; then
  step "go test -tags=integration (POLI_PAGE_API_KEY set)"
  go test -tags=integration -race -count=1 -timeout=300s ./...
  ok "integration tests pass"
else
  warn "POLI_PAGE_API_KEY not set — skipping integration tests"
fi

if [[ -n "${POLI_PAGE_API_KEY:-}" ]]; then
  step "go run ./cmd/demo (end-to-end smoke)"
  NO_COLOR=1 go run ./cmd/demo
  ok "demo completed all 10 steps"
else
  warn "POLI_PAGE_API_KEY not set — skipping demo smoke"
fi

# ─── confirm + tag ──────────────────────────────────────────────────────

if [[ "$dry_run" -eq 1 ]]; then
  step "Dry run complete"
  ok "would have tagged $version on $(git rev-parse --short HEAD)"
  echo
  echo "  Re-run without --dry-run to tag locally."
  echo "  Push manually with: ${bold}git push origin $version${reset}"
  exit 0
fi

step "Tag $version"
echo
echo "  About to create tag ${bold}$version${reset} on $(git rev-parse --short HEAD)"
echo "  ${dim}(no push — that's a separate manual git push origin $version)${reset}"
echo
read -r -p "  Continue? [y/N] " answer
if [[ "$answer" != "y" && "$answer" != "Y" ]]; then
  fail "aborted by user"
fi

git tag -a "$version" -m "Release $version"
ok "tagged $version locally"
echo
echo "  Next step (when you're ready): ${bold}git push origin $version${reset}"
echo "  proxy.golang.org will index it within ~1 minute;"
echo "  sum.golang.org will record the checksum for transparency."
