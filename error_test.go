package polipage

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestError_implementsErrorInterface(t *testing.T) {
	t.Parallel()
	var _ error = &Error{}
}

func TestError_messageFormat(t *testing.T) {
	t.Parallel()
	e := &Error{Code: "VALIDATION_ERROR", StatusCode: 400, Message: "data is required"}
	got := e.Error()
	for _, want := range []string{"polipage", "VALIDATION_ERROR", "400", "data is required"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, want to contain %q", got, want)
		}
	}
}

func TestError_messageOmitsBracketWhenStatusZero(t *testing.T) {
	t.Parallel()
	e := &Error{Code: "network_error", Message: "dial tcp: connection refused"}
	got := e.Error()
	if strings.Contains(got, "[0]") {
		t.Errorf("Error() = %q, should not include [0] when StatusCode == 0", got)
	}
	if !strings.Contains(got, "network_error") || !strings.Contains(got, "dial tcp: connection refused") {
		t.Errorf("Error() = %q, missing code or message", got)
	}
}

func TestError_unwrapReturnsCause(t *testing.T) {
	t.Parallel()
	cause := io.EOF
	e := &Error{Code: "network_error", Cause: cause}
	if got := errors.Unwrap(e); got != cause {
		t.Fatalf("errors.Unwrap = %v, want %v", got, cause)
	}
}

func TestError_errorsAs(t *testing.T) {
	t.Parallel()
	var err error = &Error{Code: "NOT_FOUND", StatusCode: 404, Message: "missing"}
	var target *Error
	if !errors.As(err, &target) {
		t.Fatal("errors.As(*Error) failed")
	}
	if target.Code != "NOT_FOUND" {
		t.Errorf("As-extracted code = %q, want NOT_FOUND", target.Code)
	}
}

func TestError_errorsIsViaSentinels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      *Error
		sentinel error
		want     bool
	}{
		{"NOT_FOUND matches ErrNotFound", &Error{Code: ErrCodeNotFound, StatusCode: 404}, ErrNotFound, true},
		{"FORBIDDEN matches ErrForbidden", &Error{Code: ErrCodeForbidden, StatusCode: 403}, ErrForbidden, true},
		{"VERSION_NOT_FOUND matches ErrVersionNotFound", &Error{Code: ErrCodeVersionNotFound, StatusCode: 404}, ErrVersionNotFound, true},
		{"GONE matches ErrGone", &Error{Code: ErrCodeGone, StatusCode: 410}, ErrGone, true},
		{"VALIDATION_ERROR matches ErrValidation", &Error{Code: ErrCodeValidationError, StatusCode: 400}, ErrValidation, true},
		{"timeout matches ErrTimeout", &Error{Code: ErrCodeTimeout}, ErrTimeout, true},
		{"aborted matches ErrAborted", &Error{Code: ErrCodeAborted}, ErrAborted, true},
		{"NOT_FOUND does NOT match ErrForbidden", &Error{Code: ErrCodeNotFound, StatusCode: 404}, ErrForbidden, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := errors.Is(tc.err, tc.sentinel); got != tc.want {
				t.Fatalf("errors.Is(%+v, sentinel) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestError_errorsIsGroupsAuthCodes(t *testing.T) {
	t.Parallel()
	// ErrUnauthorized must match both MISSING_API_KEY and INVALID_API_KEY
	// per sdk-go-plan.md §7.0.
	for _, code := range []string{ErrCodeMissingAPIKey, ErrCodeInvalidAPIKey} {
		e := &Error{Code: code, StatusCode: 401}
		if !errors.Is(e, ErrUnauthorized) {
			t.Errorf("errors.Is(code=%s, ErrUnauthorized) = false, want true", code)
		}
	}
}

func TestError_errorsIsGroupsRateLimitCodes(t *testing.T) {
	t.Parallel()
	// ErrRateLimit must match both QUOTA_EXCEEDED and OVERAGE_CAP_EXCEEDED
	// per sdk-go-plan.md §7.0.
	for _, code := range []string{ErrCodeQuotaExceeded, ErrCodeOverageCapExceeded} {
		e := &Error{Code: code, StatusCode: 429}
		if !errors.Is(e, ErrRateLimit) {
			t.Errorf("errors.Is(code=%s, ErrRateLimit) = false, want true", code)
		}
	}
}

func TestError_errorsIsUnwrapsCause(t *testing.T) {
	t.Parallel()
	cause := fmt.Errorf("wrapped: %w", io.EOF)
	e := &Error{Code: "network_error", Cause: cause}
	if !errors.Is(e, io.EOF) {
		t.Fatal("errors.Is(*Error wrapping io.EOF, io.EOF) = false, want true")
	}
}

func TestError_isAuthError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status int
		want   bool
	}{{401, true}, {403, true}, {404, false}, {400, false}, {500, false}, {0, false}}
	for _, c := range cases {
		e := &Error{StatusCode: c.status}
		if got := e.IsAuthError(); got != c.want {
			t.Errorf("IsAuthError(status=%d) = %v, want %v", c.status, got, c.want)
		}
	}
}

func TestError_isRateLimitError(t *testing.T) {
	t.Parallel()
	if !(&Error{StatusCode: 429}).IsRateLimitError() {
		t.Error("IsRateLimitError(429) = false, want true")
	}
	if (&Error{StatusCode: 500}).IsRateLimitError() {
		t.Error("IsRateLimitError(500) = true, want false")
	}
}

func TestError_isValidationError(t *testing.T) {
	t.Parallel()
	if !(&Error{StatusCode: 400}).IsValidationError() {
		t.Error("IsValidationError(400) = false, want true")
	}
	if (&Error{StatusCode: 401}).IsValidationError() {
		t.Error("IsValidationError(401) = true, want false")
	}
}

func TestError_isNetworkError(t *testing.T) {
	t.Parallel()
	if !(&Error{Code: ErrCodeNetworkError}).IsNetworkError() {
		t.Error("IsNetworkError(network_error) = false, want true")
	}
	if !(&Error{Code: ErrCodeTimeout}).IsNetworkError() {
		t.Error("IsNetworkError(timeout) = false, want true")
	}
	if (&Error{Code: ErrCodeAborted}).IsNetworkError() {
		t.Error("IsNetworkError(aborted) = true, want false")
	}
	if (&Error{Code: ErrCodeInvalidOptions}).IsNetworkError() {
		t.Error("IsNetworkError(invalid_options) = true, want false")
	}
	if (&Error{Code: "INTERNAL_ERROR", StatusCode: 500}).IsNetworkError() {
		t.Error("IsNetworkError(INTERNAL_ERROR/500) = true, want false")
	}
}

func TestError_isRetryable(t *testing.T) {
	t.Parallel()
	retryable := []*Error{
		{Code: "INTERNAL_ERROR", StatusCode: 500},
		{Code: "INTERNAL_ERROR", StatusCode: 502},
		{Code: "rate_limited", StatusCode: 429},
		{Code: ErrCodeNetworkError},
		{Code: ErrCodeTimeout},
	}
	notRetryable := []*Error{
		{Code: ErrCodeValidationError, StatusCode: 400},
		{Code: ErrCodeAborted},
	}
	for _, e := range retryable {
		if !e.IsRetryable() {
			t.Errorf("IsRetryable(%+v) = false, want true", e)
		}
	}
	for _, e := range notRetryable {
		if e.IsRetryable() {
			t.Errorf("IsRetryable(%+v) = true, want false", e)
		}
	}
}
