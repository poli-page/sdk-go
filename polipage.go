// Package polipage is the official Go SDK for [Poli Page].
//
// Phase 0 scaffold — the public surface is intentionally empty until Phase 2
// of the build plan. See sdk-go-plan.md for the full roadmap.
//
// [Poli Page]: https://poli.page
package polipage

// Client is the Poli Page SDK entry point.
//
// Construct one via [NewClient] and reuse it for the lifetime of the process —
// the underlying *http.Client pools connections automatically.
type Client struct{}

// NewClient constructs a Poli Page SDK client.
//
// This is a Phase 0 stub. Functional options (option.WithAPIKey, etc.) land in
// Phase 2.
func NewClient() *Client {
	return &Client{}
}
