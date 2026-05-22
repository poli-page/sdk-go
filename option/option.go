// Package option holds the functional options for constructing a
// polipage.Client and overriding behavior on individual method calls.
//
// Construction-only options (WithAPIKey, WithMaxRetries, WithLogger, ...)
// are passed to polipage.NewClient. Per-call options (WithIdempotencyKey)
// are passed as the variadic last argument on any method that accepts
// option.RequestOption.
package option

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/poli-page/sdk-go/internal/clientconfig"
)

// RequestOption mutates a [clientconfig.Config]. NewClient applies options
// in order; later options override earlier ones for the same field.
type RequestOption func(*clientconfig.Config) error

// RetryEvent is re-exported from the underlying [clientconfig.RetryEvent]
// so users importing only the option package can spell the hook signature
// without reaching for polipage.RetryEvent.
type RetryEvent = clientconfig.RetryEvent

// WithAPIKey sets the Poli Page API key used in the Authorization header.
// Required — NewClient surfaces an *Error with Code == "invalid_options"
// on the first method call if no key was supplied.
func WithAPIKey(key string) RequestOption {
	return func(c *clientconfig.Config) error {
		c.APIKey = key
		return nil
	}
}

// WithBaseURL overrides the API base URL. Useful for pointing at
// api-develop.poli.page during integration testing.
func WithBaseURL(url string) RequestOption {
	return func(c *clientconfig.Config) error {
		c.BaseURL = url
		return nil
	}
}

// WithMaxRetries sets the retry budget on top of the initial attempt.
// 0 disables retries (single attempt). Default 2 (3 attempts total).
func WithMaxRetries(n int) RequestOption {
	return func(c *clientconfig.Config) error {
		c.MaxRetries = n
		return nil
	}
}

// WithRetryDelay sets the base exponential-backoff delay. The first retry
// waits ~d × jitter, the second ~2d × jitter, etc. Default 500ms.
func WithRetryDelay(d time.Duration) RequestOption {
	return func(c *clientconfig.Config) error {
		c.RetryDelay = d
		return nil
	}
}

// WithTimeout sets the per-request fallback deadline. Only applies when
// the caller's context has no deadline of its own. Default 60s.
func WithTimeout(d time.Duration) RequestOption {
	return func(c *clientconfig.Config) error {
		c.Timeout = d
		return nil
	}
}

// WithHTTPClient injects a custom *http.Client. The SDK uses it as-is and
// does not touch its Timeout field — the SDK's WithTimeout becomes a
// context.WithTimeout default instead.
func WithHTTPClient(client *http.Client) RequestOption {
	return func(c *clientconfig.Config) error {
		c.HTTPClient = client
		return nil
	}
}

// WithLogger sets a structured logger that receives one DEBUG line per
// HTTP attempt, one WARN per retry, and one ERROR per terminal failure.
// Default is slog.New(slog.DiscardHandler{}) — silent.
func WithLogger(l *slog.Logger) RequestOption {
	return func(c *clientconfig.Config) error {
		c.Logger = l
		return nil
	}
}

// WithOnRetry registers a hook that fires before each retry sleep, with
// the attempt number, delay, and the error that triggered the retry.
// Panics inside the hook are recovered — hooks never break the request.
func WithOnRetry(fn func(RetryEvent)) RequestOption {
	return func(c *clientconfig.Config) error {
		c.OnRetry = fn
		return nil
	}
}

// WithOnError registers a hook that fires once at terminal failure (retries
// exhausted, non-retryable error, or aborted). The argument is the *Error
// returned to the caller — use errors.As to extract typed fields.
// Panics inside the hook are recovered.
func WithOnError(fn func(error)) RequestOption {
	return func(c *clientconfig.Config) error {
		c.OnError = fn
		return nil
	}
}

// WithIdempotencyKey overrides the auto-generated UUID4 Idempotency-Key
// header on a POST request. Pass this as the last argument to a render or
// thumbnails call when the caller has a natural idempotency identifier
// (e.g. an invoice number).
func WithIdempotencyKey(key string) RequestOption {
	return func(c *clientconfig.Config) error {
		c.IdempotencyKey = key
		return nil
	}
}

// WithRequestTimeout overrides the per-request deadline for a single call.
// Pass as the last argument to any method to override the client-wide
// [WithTimeout] for that call only. When the caller's context already has
// a deadline, this override is ignored — context wins.
//
// Setting WithRequestTimeout at construction time also adjusts the client
// default, identically to [WithTimeout].
func WithRequestTimeout(d time.Duration) RequestOption {
	return func(c *clientconfig.Config) error {
		c.Timeout = d
		return nil
	}
}

// WithHeader attaches an extra HTTP header to outgoing requests. Pass at
// construction time to add the header to every request, or as a per-call
// last argument to override / add per-request. Per-call entries override
// construction-time entries for the same key.
//
// Useful for tracing IDs, custom auth proxy tokens, or feature flags. The
// SDK's own headers (Authorization, Content-Type, Accept, User-Agent,
// Idempotency-Key) are written first; if a caller-supplied key matches one
// of these, the caller wins — use with care.
func WithHeader(key, value string) RequestOption {
	return func(c *clientconfig.Config) error {
		if c.Headers == nil {
			c.Headers = make(map[string]string, 1)
		}
		c.Headers[key] = value
		return nil
	}
}
