// Package clientconfig holds the mutable Config used by the polipage
// client and the option subpackage. Lives under internal/ to keep its
// shape an implementation detail — callers go through option.With* to
// build a Config and through polipage.NewClient to consume it.
package clientconfig

import (
	"log/slog"
	"net/http"
	"time"
)

// RetryEvent fires before each retry sleep. Re-exported from the polipage
// and option packages as a type alias so callers can spell it either way.
type RetryEvent struct {
	// Attempt is 1-based; this is the attempt that is about to run.
	Attempt int
	// DelayMs is the sleep duration before this attempt, in milliseconds.
	DelayMs float64
	// Reason is the error that triggered the retry. Always a *polipage.Error
	// at runtime — typed as plain error here so this package does not have to
	// import polipage.
	Reason error
}

// Config is the resolved client + per-call options. All fields are populated
// from the Default constructor plus zero or more With* mutators.
type Config struct {
	APIKey     string
	BaseURL    string
	MaxRetries int
	RetryDelay time.Duration
	Timeout    time.Duration
	HTTPClient *http.Client
	Logger     *slog.Logger
	OnRetry    func(RetryEvent)
	OnError    func(error)

	// IdempotencyKey is per-call only. Empty in the construction-time Config.
	IdempotencyKey string

	// Headers are extra HTTP headers attached to outgoing requests. Both
	// construction-time and per-call values are merged into the final
	// request header set, with per-call entries overriding construction-time
	// entries for the same key. Per-call overrides win over the SDK's own
	// headers too — caller's responsibility.
	Headers map[string]string
}

// MergePerCall returns a Config holding only the per-call overrides from a
// slice of options. Construction-time defaults are NOT folded in — the
// orchestrator combines them with the client's resolved Config at request
// time.
func MergePerCall(apply func(*Config) error) (Config, error) {
	var c Config
	if err := apply(&c); err != nil {
		return c, err
	}
	return c, nil
}

// Default returns a Config seeded with the package defaults. Constants come
// from polipage's user-facing Default* names (kept literal here to avoid
// importing polipage).
func Default() Config {
	return Config{
		BaseURL:    "https://api.poli.page",
		MaxRetries: 2,
		RetryDelay: 500 * time.Millisecond,
		Timeout:    60 * time.Second,
	}
}
