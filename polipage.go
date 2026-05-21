// Package polipage is the official Go SDK for [Poli Page].
//
// The transport core (URL/header building, retry math, error parsing) and
// the *Error type are exported as of Phase 1. The full *Client orchestration
// + Render/Documents namespaces land in subsequent phases per sdk-go-plan.md.
//
// [Poli Page]: https://poli.page
package polipage

import "time"

// User-facing default values for client construction. Internal SDK
// values (header names, jitter bounds, path templates) live in
// internal/constants instead, per sdk-go-plan.md §17.
const (
	// DefaultBaseURL is the production Poli Page API base URL. Override via
	// option.WithBaseURL when targeting api-develop.poli.page or another
	// environment.
	DefaultBaseURL = "https://api.poli.page"

	// DefaultMaxRetries is the default retry budget: 2 retries on top of the
	// initial attempt, so 3 attempts total before a retryable error surfaces.
	DefaultMaxRetries = 2

	// DefaultRetryDelay is the base exponential-backoff delay. The first
	// retry waits ~DefaultRetryDelay × jitter, the second ~2×, etc.
	DefaultRetryDelay = 500 * time.Millisecond

	// DefaultTimeout is the per-request fallback deadline applied when the
	// caller's context has no deadline of its own.
	DefaultTimeout = 60 * time.Second
)

// Client is the Poli Page SDK entry point.
//
// Construct one via [NewClient] and reuse it for the lifetime of the process —
// the underlying *http.Client pools connections automatically.
type Client struct{}

// NewClient constructs a Poli Page SDK client.
//
// Phase 1 stub: functional options (option.WithAPIKey, etc.), retry loop
// orchestration, and the Render / Documents namespaces land in Phase 2+.
func NewClient() *Client {
	return &Client{}
}
