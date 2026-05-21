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
