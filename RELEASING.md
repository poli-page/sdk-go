# Releasing

## Versions

`github.com/poli-page/sdk-go` shares its major.minor with the Poli Page API:
SDK `0.13.x` talks to API `0.13` and passes the
[API contract](https://github.com/poli-page/api-contract) `v0.13.0`, and is
released once the API runs that version. Patch numbers are free: a fix to
the SDK alone is a patch, whatever the API's patch. A change of the API
contract, a new SDK feature and a raise of the minimum Go version are each
a minor. The SDK may skip minors when it has nothing to ship for one. The
module path stays `github.com/poli-page/sdk-go` while the major is 0.

Both `scripts/release.sh` and the Release workflow refuse a tag whose
major.minor differs from the contract version pinned in
`.github/workflows/ci.yml`. A Go release is the pushed tag itself, so
`scripts/release.sh` is the gate; the workflow re-checks the tag once it is
pushed, then creates the GitHub Release from the CHANGELOG section.

## Steps

The release steps, from the version bump to the tag push, are in
[CONTRIBUTING.md](CONTRIBUTING.md#releasing).
