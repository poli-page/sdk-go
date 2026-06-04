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
		initErr = &Error{Code: ErrCodeInvalidOptions, Message: "option.WithAPIKey is required"}
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

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := httpx.ComputeBackoff(attempt, c.cfg.RetryDelay, nextRetryAfter)
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
		nextRetryAfter = retryAfter
		if !retryable {
			c.fireOnError(err)
			return nil, err
		}
	}

	c.fireOnError(lastErr)
	return nil, lastErr
}

// sendOnce performs a single HTTP attempt. Returns (resp, 0, false, nil) on
// 2xx; (nil, retryAfter, retryable, err) otherwise.
//
// When a per-attempt timeout context is created, ownership of cancel() is
// transferred to the returned resp.Body via cancelOnClose — closing the
// body cancels the context. This avoids the trap where deferring cancel()
// inside sendOnce kills the body mid-read.
func (c *Client) sendOnce(ctx context.Context, method, path string, bodyBytes []byte, per clientconfig.Config, attempt int) (*http.Response, time.Duration, bool, *Error) {
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
		return nil, 0, false, &Error{Code: ErrCodeInvalidOptions, Message: "failed to build request: " + err.Error(), Cause: err}
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

	c.fireOnRequest(clientconfig.RequestEvent{
		Method:  method,
		URL:     url,
		Attempt: attempt,
	})

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		cleanup()
		if ctx.Err() != nil {
			return nil, 0, false, mapContextError(ctx.Err())
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, 0, true, &Error{Code: ErrCodeTimeout, Message: fmt.Sprintf("request timed out after %v", timeout), Cause: err}
		}
		return nil, 0, true, &Error{Code: ErrCodeNetworkError, Message: err.Error(), Cause: err}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if cancel != nil {
			// Transfer ownership of cancel to the body: the caller cancels
			// the per-attempt context when they close the body.
			resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
		}
		return resp, 0, false, nil
	}

	// non-2xx — parse body, classify, return *Error.
	defer cleanup()
	defer func() { _ = resp.Body.Close() }()
	bodyOut, _ := io.ReadAll(resp.Body)
	code, message := httpx.ParseErrorBody(bodyOut, resp.StatusCode)
	requestID := resp.Header.Get(constants.HeaderRequestID)
	retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
	var retryAfter time.Duration
	if retryable {
		if d, ok := httpx.ParseRetryAfter(resp.Header.Get(constants.HeaderRetryAfter)); ok {
			retryAfter = d
		}
	}
	return nil, retryAfter, retryable, &Error{
		Code:       code,
		StatusCode: resp.StatusCode,
		Message:    message,
		RequestID:  requestID,
	}
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

// cancelOnClose wraps an http.Response.Body so that closing it also
// cancels the per-attempt context. Solves the standard Go pitfall of
// deferring cancel() in the request-building helper, which kills the body
// before the caller can read it.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// mapContextError turns a context error into an *Error with the right Code.
func mapContextError(err error) *Error {
	if errors.Is(err, context.Canceled) {
		return &Error{Code: ErrCodeAborted, Message: "request was aborted", Cause: err}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: ErrCodeTimeout, Message: "request deadline exceeded", Cause: err}
	}
	return &Error{Code: ErrCodeNetworkError, Message: err.Error(), Cause: err}
}

// sleepCtx blocks for d, returning an *Error with Code == ErrCodeAborted (or
// ErrCodeTimeout) if the context is canceled or expires before the sleep
// completes.
func sleepCtx(ctx context.Context, d time.Duration) *Error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return mapContextError(ctx.Err())
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
