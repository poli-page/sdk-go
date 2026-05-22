#!/usr/bin/env bash
# install-hooks.sh — write .git/hooks/pre-push for this repo.
#
# Idempotent: re-running overwrites the hook with the latest version.
#
# The pre-push hook runs:
#   - gofmt -l .          (fails if any file would be reformatted)
#   - go vet ./...
#   - golangci-lint run   (skipped if golangci-lint isn't installed)
#   - go test -race ./... (unit tests)
#
# Integration tests are skipped unless RUN_INTEGRATION=1 is set in the
# environment of the `git push` invocation.

set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
hook_path="$repo_root/.git/hooks/pre-push"

cat > "$hook_path" <<'HOOK'
#!/usr/bin/env bash
# Poli Page SDK pre-push hook. Installed by scripts/install-hooks.sh.
# Bypass with `git push --no-verify` (don't make a habit of it).

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

echo "▶ gofmt -l ."
unformatted=$(gofmt -l .)
if [[ -n "$unformatted" ]]; then
  echo "✗ the following files are not gofmt-clean:"
  echo "$unformatted"
  exit 1
fi

echo "▶ go vet ./..."
go vet ./...

if command -v golangci-lint >/dev/null 2>&1; then
  echo "▶ golangci-lint run ./..."
  golangci-lint run ./...
else
  echo "⚠ golangci-lint not on PATH — skipping"
fi

echo "▶ go test -race ./..."
go test -race ./...

if [[ "${RUN_INTEGRATION:-0}" == "1" ]]; then
  echo "▶ go test -tags=integration -race -count=1 ./..."
  go test -tags=integration -race -count=1 -timeout=300s ./...
fi

echo "✔ pre-push checks passed"
HOOK

chmod +x "$hook_path"
echo "✔ installed pre-push hook at $hook_path"
echo "  Bypass once with: git push --no-verify"
echo "  Run integration tests on push: RUN_INTEGRATION=1 git push"
