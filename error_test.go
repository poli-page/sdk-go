package polipage

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"slices"
	"strconv"
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
		{"DOCUMENT_GONE matches ErrDocumentGone", &Error{Code: ErrCodeDocumentGone, StatusCode: 410}, ErrDocumentGone, true},
		{"ORGANIZATION_PURGED does NOT match ErrDocumentGone (distinct 410 codes)", &Error{Code: ErrCodeOrganizationPurged, StatusCode: 410}, ErrDocumentGone, false},
		{"VALIDATION_ERROR matches ErrValidation", &Error{Code: ErrCodeValidationError, StatusCode: 400}, ErrValidation, true},
		{"timeout matches ErrTimeout", &Error{Code: ErrCodeTimeout}, ErrTimeout, true},
		{"aborted matches ErrAborted", &Error{Code: ErrCodeAborted}, ErrAborted, true},
		{"NOT_FOUND does NOT match ErrForbidden", &Error{Code: ErrCodeNotFound, StatusCode: 404}, ErrForbidden, false},
		{"ORGANIZATION_CANCELLED matches ErrOrganizationCancelled", &Error{Code: ErrCodeOrganizationCancelled, StatusCode: 403}, ErrOrganizationCancelled, true},
		{"ORGANIZATION_PURGED matches ErrOrganizationPurged", &Error{Code: ErrCodeOrganizationPurged, StatusCode: 410}, ErrOrganizationPurged, true},
		{"ORGANIZATION_PAYMENT_GRACE does NOT match ErrForbidden (distinct lifecycle codes)", &Error{Code: ErrCodeOrganizationPaymentGrace, StatusCode: 403}, ErrForbidden, false},
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
		{Code: ErrCodeRateLimitExceeded, StatusCode: 429},
		{Code: ErrCodeDownloadFailed, StatusCode: 503},
		{Code: ErrCodeNetworkError},
		{Code: ErrCodeTimeout},
	}
	notRetryable := []*Error{
		{Code: ErrCodeValidationError, StatusCode: 400},
		{Code: ErrCodeAborted},
		// Waiting cannot fix these, whatever their status.
		{Code: ErrCodeQuotaExceeded, StatusCode: 429},
		{Code: ErrCodeOverageCapExceeded, StatusCode: 429},
		{Code: ErrCodeStorageAccessDenied, StatusCode: 502},
		// A download that got no answer.
		{Code: ErrCodeDownloadFailed},
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

// TestErrorCodes_constantSetMatchesContract reads the ErrCode* constants
// declared in error.go and checks their wire values are exactly the
// contract's 31 API codes plus the SDK-internal codes: a removed code
// left behind or a contract code missing both fail here.
func TestErrorCodes_constantSetMatchesContract(t *testing.T) {
	t.Parallel()
	file, err := parser.ParseFile(token.NewFileSet(), "error.go", nil, 0)
	if err != nil {
		t.Fatalf("parse error.go: %v", err)
	}
	var got []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !strings.HasPrefix(name.Name, "ErrCode") || !ok {
					continue
				}
				v, _ := strconv.Unquote(lit.Value)
				if !slices.Contains(got, v) {
					got = append(got, v)
				}
			}
		}
	}
	want := []string{
		// SDK-internal.
		"invalid_options", "network_error", "timeout", "aborted", "unknown_error", "DOWNLOAD_FAILED", "io_failed",
		// API codes kept from 0.9.
		"MISSING_API_KEY", "INVALID_API_KEY", "ORGANIZATION_CANCELLED", "ORGANIZATION_PURGED", "FORBIDDEN",
		"VALIDATION_ERROR", "MISSING_DATA", "MISSING_TEMPLATE_SLUG", "PROJECT_REQUIRED_FOR_DOCUMENT",
		"NOT_FOUND", "DOCUMENT_NOT_FOUND", "QUOTA_EXCEEDED", "OVERAGE_CAP_EXCEEDED", "INTERNAL_ERROR",
		// API codes added with environments.
		"PUBLISH_IN_PROGRESS", "ENVIRONMENT_MISMATCH", "PROJECT_NOT_IN_ENVIRONMENT", "TEMPLATE_NOT_FOUND",
		"DOCUMENT_GONE", "ENVIRONMENT_PROTECTED", "ORGANIZATION_PAYMENT_GRACE", "EMPTY_DOCUMENT",
		"STORAGE_ACCESS_DENIED", "ORGANIZATION_MIGRATING", "PROJECT_NOT_FOUND", "TEMPLATE_ERROR", "TEMPLATE_UNSAFE",
		"THUMBNAILS_NOT_AVAILABLE", "RATE_LIMIT_EXCEEDED", "RENDER_BODY_TOO_LARGE", "DOCUMENT_TOO_LARGE",
		"RENDER_TIMEOUT",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("ErrCode* wire values =\n%v\nwant\n%v", got, want)
	}
}
