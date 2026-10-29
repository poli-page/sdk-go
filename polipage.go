// Package polipage is the official Go SDK for [Poli Page].
//
// Construct a client with [NewClient], then call methods on the [Render]
// or Documents namespaces. The client is safe for concurrent use — share
// one instance per process so the underlying *http.Client pools
// connections automatically.
//
// [Poli Page]: https://poli.page
package polipage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/poli-page/sdk-go/internal/clientconfig"
	"github.com/poli-page/sdk-go/internal/constants"
	httpx "github.com/poli-page/sdk-go/internal/http"
	"github.com/poli-page/sdk-go/internal/version"
	"github.com/poli-page/sdk-go/option"
)

// User-facing default values for client construction. Internal SDK
// values (header names, jitter bounds, path templates) live in
// internal/constants instead, per sdk-go-plan.md §17.
const (
	// DefaultBaseURL is the production Poli Page API base URL. Override via
	// option.WithBaseURL to target another API host.
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

// Client is the Poli Page SDK entry point. Construct one via [NewClient]
// and reuse it for the lifetime of the process.
type Client struct {
	cfg     clientconfig.Config
	initErr error

	// Render is the render namespace. See [Render] for available methods.
	Render *Render

	// Documents is the stored-documents namespace. See [Documents] for
	// available methods.
	Documents *Documents
}

// NewClient constructs a Poli Page SDK client. Options are applied in order;
// later options override earlier ones for the same field.
//
// Validation is deferred: NewClient never returns an error. If a required
// option is missing (e.g. [option.WithAPIKey]), the first method call
// returns an *Error with Code == ErrCodeInvalidOptions.
//
//	client := polipage.NewClient(
//	    option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
//	)
func NewClient(opts ...option.RequestOption) *Client {
	cfg := clientconfig.Default()
	var initErr error
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			initErr = err
			break
		}
	}
	if initErr == nil && cfg.APIKey == "" {
		initErr = &Error{Code: ErrCodeInvalidOptions, Message: "apiKey is required"}
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Transport: defaultTransport()}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	c := &Client{cfg: cfg, initErr: initErr}
	c.Render = &Render{client: c}
	c.Documents = &Documents{client: c}
	return c
}

// userAgent returns the User-Agent header value for this build.
func (c *Client) userAgent() string {
	return "poli-page-sdk-go/" + version.Version
}

// transport is the internal seam each namespace uses to issue HTTP
// requests. *Client is the production implementer; the interface stays
// around as a documented future mock-point per sdk-go-plan.md §3.2.
//
// per carries per-call overrides resolved from variadic option.RequestOption
// values (IdempotencyKey, Timeout, Headers). Fields left zero defer to the
// client's resolved Config.
type transport interface {
	post(ctx context.Context, path string, body any, per clientconfig.Config) (*http.Response, error)
	get(ctx context.Context, path string, per clientconfig.Config) (*http.Response, error)
	delete(ctx context.Context, path string, per clientconfig.Config) error
}

// Compile-time assertion that *Client satisfies the transport seam.
var _ transport = (*Client)(nil)

func (c *Client) post(ctx context.Context, path string, body any, per clientconfig.Config) (*http.Response, error) {
	return c.do(ctx, http.MethodPost, path, body, per)
}

func (c *Client) get(ctx context.Context, path string, per clientconfig.Config) (*http.Response, error) {
	return c.do(ctx, http.MethodGet, path, nil, per)
}

func (c *Client) delete(ctx context.Context, path string, per clientconfig.Config) error {
	resp, err := c.do(ctx, http.MethodDelete, path, nil, per)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return nil
}

// do is the orchestrator: it runs the request with retries, hook firing,
// per-attempt timeout, and full error mapping. Callers responsible for
// closing resp.Body on success.
func (c *Client) do(ctx context.Context, method, path string, body any, per clientconfig.Config) (*http.Response, error) {
	if c.initErr != nil {
		c.fireOnError(c.initErr)
		return nil, c.initErr
	}
	if err := ctx.Err(); err != nil {
		mapped := mapContextError(err)
		c.fireOnError(mapped)
		return nil, mapped
	}

	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			mapped := &Error{Code: ErrCodeInvalidOptions, Message: "failed to marshal request body: " + err.Error(), Cause: err}
			c.fireOnError(mapped)
			return nil, mapped
		}
		bodyBytes = b
	}

	var lastErr *Error
	var nextRetryAfter time.Duration
	var hasRetryAfter bool

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// A Retry-After header (even "0") sets the delay, without jitter.
			delay := nextRetryAfter
			if !hasRetryAfter {
				delay = httpx.ComputeBackoff(attempt, c.cfg.RetryDelay)
			}
			c.fireOnRetry(clientconfig.RetryEvent{
				Attempt: attempt + 1,
				DelayMs: float64(delay) / float64(time.Millisecond),
				Reason:  lastErr,
			})
			if err := sleepCtx(ctx, delay); err != nil {
				c.fireOnError(err)
				return nil, err
			}
		}

		resp, retryAfter, retryable, err := c.sendOnce(ctx, method, path, bodyBytes, per, attempt+1)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		nextRetryAfter, hasRetryAfter = retryAfter.delay, retryAfter.ok
		if !retryable {
			c.fireOnError(err)
			return nil, err
		}
	}

	c.fireOnError(lastErr)
	return nil, lastErr
}

// retryAfter is the delay a failed answer's Retry-After header asks for;
// ok is false when the header is absent or unparseable.
type retryAfter struct {
	delay time.Duration
	ok    bool
}

// sendOnce performs a single HTTP attempt. Returns (resp, {}, false, nil) on
// 2xx, with resp.Body already read into memory; (nil, retryAfter,
// retryable, err) otherwise.
//
// A 2xx body is read inside the attempt, under its timeout: a body that
// drops or times out mid-read is this attempt's network_error or timeout,
// retried like any other (spec §9.1). API answers are small JSON or HTML;
// PDF bytes come from the presigned URL, never through here.
func (c *Client) sendOnce(ctx context.Context, method, path string, bodyBytes []byte, per clientconfig.Config, attempt int) (*http.Response, retryAfter, bool, *Error) {
	timeout := c.cfg.Timeout
	if per.Timeout > 0 {
		timeout = per.Timeout
	}
	attemptCtx := ctx
	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		attemptCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	cleanup := func() {
		if cancel != nil {
			cancel()
		}
	}

	url := httpx.BuildURL(c.cfg.BaseURL, path)
	var body io.Reader
	if bodyBytes != nil {
		body = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(attemptCtx, method, url, body)
	if err != nil {
		cleanup()
		return nil, retryAfter{}, false, &Error{Code: ErrCodeInvalidOptions, Message: "failed to build request: " + err.Error(), Cause: err}
	}
	if bodyBytes != nil {
		// net/http's Transport silently resends a "replayable" request once
		// when a reused keep-alive connection dies before any answer, and a
		// POST with GetBody and an Idempotency-Key header counts as one.
		// The API does not deduplicate on that key, so a hidden resend could
		// store a second document. Without GetBody the failure surfaces as
		// network_error and goes through the SDK's own retry: counted by
		// maxRetries, reported to OnRequest and OnRetry.
		req.GetBody = nil
	}
	req.Header = httpx.BuildHeaders(method, c.cfg.APIKey, per.IdempotencyKey, c.userAgent())
	// Construction-time custom headers, then per-call overrides. Caller's
	// keys may overwrite the SDK's own; documented in option.WithHeader.
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}
	for k, v := range per.Headers {
		req.Header.Set(k, v)
	}

	c.cfg.Logger.LogAttrs(ctx, slog.LevelDebug, "polipage: http attempt",
		slog.String("method", method),
		slog.String("url", url),
		slog.Int("attempt", attempt),
	)

	t0 := time.Now()
	c.fireOnRequest(clientconfig.RequestEvent{
		Method:  method,
		URL:     url,
		Attempt: attempt,
	})

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		cleanup()
		if ctx.Err() != nil {
			return nil, retryAfter{}, false, mapContextError(ctx.Err())
		}
		// The messages are the SDK specification's exact texts; the
		// transport error stays available as Cause.
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, retryAfter{}, true, &Error{Code: ErrCodeTimeout, Message: timeoutMessage(timeout), Cause: err}
		}
		return nil, retryAfter{}, true, &Error{Code: ErrCodeNetworkError, Message: "Network request failed", Cause: err}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		timedOut := errors.Is(attemptCtx.Err(), context.DeadlineExceeded)
		cleanup()
		if readErr != nil {
			if ctx.Err() != nil {
				return nil, retryAfter{}, false, mapContextError(ctx.Err())
			}
			if timedOut || errors.Is(readErr, context.DeadlineExceeded) {
				return nil, retryAfter{}, true, &Error{Code: ErrCodeTimeout, Message: timeoutMessage(timeout), Cause: readErr}
			}
			return nil, retryAfter{}, true, &Error{Code: ErrCodeNetworkError, Message: "Network request failed", Cause: readErr}
		}
		c.fireOnResponse(clientconfig.ResponseEvent{
			Status:     resp.StatusCode,
			RequestID:  resp.Header.Get(constants.HeaderRequestID),
			DurationMs: time.Since(t0).Milliseconds(),
		})
		resp.Body = io.NopCloser(bytes.NewReader(data))
		return resp, retryAfter{}, false, nil
	}

	// non-2xx — parse body, classify, return *Error.
	defer cleanup()
	defer func() { _ = resp.Body.Close() }()
	bodyOut, readErr := io.ReadAll(resp.Body)
	if readErr != nil && ctx.Err() != nil {
		// The caller canceled while the error body was read: aborted at
		// once, never retried, whatever the status.
		return nil, retryAfter{}, false, mapContextError(ctx.Err())
	}
	var code, message string
	if readErr != nil {
		// A body that failed mid-read is malformed, even when the bytes
		// received so far parse as JSON: treat it as a non-JSON body
		// (INTERNAL_ERROR, "HTTP <status>"), keep the status so the retry
		// rule still applies, and keep the read error as Cause.
		code, message = ErrCodeInternalError, fmt.Sprintf("HTTP %d", resp.StatusCode)
	} else {
		code, message = httpx.ParseErrorBody(bodyOut, resp.StatusCode)
	}
	apiErr := &Error{
		Code:       code,
		StatusCode: resp.StatusCode,
		Message:    message,
		RequestID:  resp.Header.Get(constants.HeaderRequestID),
		Cause:      readErr,
	}
	// One rule for the retry loop and for IsRetryable: 5xx and 429 are
	// retried, except the codes waiting cannot fix (quota, storage access).
	// Only Retry-After is read; Retry-After-Ms is ignored.
	retryable := apiErr.IsRetryable()
	var after retryAfter
	if retryable {
		after.delay, after.ok = httpx.ParseRetryAfter(resp.Header.Get(constants.HeaderRetryAfter))
	}
	return nil, after, retryable, apiErr
}

// bodyReadError maps a failure decoding a 2xx API body, which sendOnce has
// already read in full: ErrCodeInternalError with the answer's status.
// Cause wraps err.
func bodyReadError(status int, what string, err error) *Error {
	return &Error{Code: ErrCodeInternalError, StatusCode: status, Message: what + err.Error(), Cause: err}
}

// timeoutMessage is the timeout error's message: the attempt's timeout in
// whole milliseconds.
func timeoutMessage(timeout time.Duration) string {
	return fmt.Sprintf("Request timed out after %dms", timeout.Milliseconds())
}

// defaultTransport returns the *http.Transport used by NewClient when the
// caller did not supply their own *http.Client. The values are tuned for
// the SDK's typical workload (1 process talking to 1 API host on HTTP/1.1
// + HTTP/2) per sdk-go-plan.md §5.2 — keep idle connections per host
// generously open so successive render calls reuse a warm connection.
//
// Callers wanting custom transport (proxies, TLS pinning, recording) pass
// their own *http.Client via [option.WithHTTPClient]; the SDK never touches
// it after that.
func defaultTransport() *http.Transport {
	// Clone the stdlib default so we inherit Proxy / DialContext / HTTP/2
	// negotiation defaults without re-implementing them.
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConnsPerHost = 10
	t.IdleConnTimeout = 90 * time.Second
	return t
}

// mapContextError turns a context error into an *Error with the right Code.
func mapContextError(err error) *Error {
	if errors.Is(err, context.Canceled) {
		return &Error{Code: ErrCodeAborted, Message: "Request was aborted", Cause: err}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: ErrCodeTimeout, Message: "request deadline exceeded", Cause: err}
	}
	return &Error{Code: ErrCodeNetworkError, Message: err.Error(), Cause: err}
}

// sleepCtx blocks for d, returning an *Error with Code == ErrCodeAborted (or
// ErrCodeTimeout) if the context is canceled or expires before the sleep
// completes.
//
// The wait is never shorter than d: should the timer fire before d has
// passed on the monotonic clock, it is re-armed for the rest.
func sleepCtx(ctx context.Context, d time.Duration) *Error {
	if d <= 0 {
		return nil
	}
	end := time.Now().Add(d) // carries a monotonic clock reading
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			left := time.Until(end)
			if left <= 0 {
				return nil
			}
			timer.Reset(left)
		case <-ctx.Done():
			return mapContextError(ctx.Err())
		}
	}
}

// fireOnRetry invokes the OnRetry hook (if any) with panic recovery.
func (c *Client) fireOnRetry(e clientconfig.RetryEvent) {
	if c.cfg.OnRetry == nil {
		return
	}
	defer func() { _ = recover() }()
	c.cfg.OnRetry(e)
}

// fireOnError invokes the OnError hook (if any) with panic recovery.
func (c *Client) fireOnError(err error) {
	if c.cfg.OnError == nil {
		return
	}
	defer func() { _ = recover() }()
	c.cfg.OnError(err)
}

// fireOnRequest invokes the OnRequest hook (if any) with panic recovery.
func (c *Client) fireOnRequest(e clientconfig.RequestEvent) {
	if c.cfg.OnRequest == nil {
		return
	}
	defer func() { _ = recover() }()
	c.cfg.OnRequest(e)
}

// fireOnResponse invokes the OnResponse hook (if any) with panic recovery.
func (c *Client) fireOnResponse(e clientconfig.ResponseEvent) {
	if c.cfg.OnResponse == nil {
		return
	}
	defer func() { _ = recover() }()
	c.cfg.OnResponse(e)
}
