package polipage_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// Port of sdk-node/tests/error-codes.test.ts. One sub-test per spec §7.2
// code, each running an httptest.Server that returns the (status, code,
// requestID) trio and asserting verbatim propagation through *Error.
//
// The point of this file is not to exercise the SDK's retry logic or
// transport — those have their own tests. It is to lock down that every
// API error code from spec §7.2 round-trips through to the caller's
// *Error with Code, StatusCode, RequestID, and Message intact.

// expectCode runs invoke against a fake server that emits the supplied
// (status, code) triplet plus a deterministic x-request-id. It asserts
// every wire field reaches the caller's *Error unchanged.
func expectCode(t *testing.T, status int, code string, invoke func(*polipage.Client) error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_test_42")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, fmt.Sprintf(`{"code":%q,"message":"Synthetic %s"}`, code, code))
	}))
	t.Cleanup(server.Close)

	client := polipage.NewClient(
		option.WithAPIKey("pp_test_abc"),
		option.WithBaseURL(server.URL),
		option.WithHTTPClient(server.Client()),
		option.WithMaxRetries(0), // no retries so 5xx/429 surface immediately
	)
	err := invoke(client)
	if err == nil {
		t.Fatalf("expected *Error for (status=%d code=%s), got nil", status, code)
	}
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		t.Fatalf("err = %v (%T), want *polipage.Error", err, err)
	}
	if pErr.Code != code {
		t.Errorf("Code = %q, want %q", pErr.Code, code)
	}
	if pErr.StatusCode != status {
		t.Errorf("StatusCode = %d, want %d", pErr.StatusCode, status)
	}
	if pErr.RequestID != "req_test_42" {
		t.Errorf("RequestID = %q, want req_test_42", pErr.RequestID)
	}
	want := fmt.Sprintf("Synthetic %s", code)
	if pErr.Message != want {
		t.Errorf("Message = %q, want %q", pErr.Message, want)
	}
}

// renderPDF is the canonical invoke used by most cases below.
func renderPDF(c *polipage.Client) error {
	_, err := c.Render.PDF(context.Background(), polipage.ProjectModeInput{
		Project: "p", Template: "t",
		Version: polipage.Opt("1.0.0"),
		Data:    map[string]any{},
	})
	return err
}

func TestErrorCodes_PAYMENT_REQUIRED_402(t *testing.T) {
	t.Parallel()
	expectCode(t, 402, polipage.ErrCodePaymentRequired, renderPDF)
}

func TestErrorCodes_ORGANIZATION_CANCELLED_403(t *testing.T) {
	t.Parallel()
	expectCode(t, 403, polipage.ErrCodeOrganizationCancelled, renderPDF)
}

func TestErrorCodes_ORGANIZATION_PURGED_410(t *testing.T) {
	t.Parallel()
	expectCode(t, 410, polipage.ErrCodeOrganizationPurged, renderPDF)
}

func TestErrorCodes_DOCUMENT_NOT_FOUND_404_fromDocumentsGet(t *testing.T) {
	t.Parallel()
	expectCode(t, 404, polipage.ErrCodeDocumentNotFound, func(c *polipage.Client) error {
		_, err := c.Documents.Get(context.Background(), "doc_missing")
		return err
	})
}

func TestErrorCodes_GONE_410_fromDocumentsGet(t *testing.T) {
	t.Parallel()
	expectCode(t, 410, polipage.ErrCodeGone, func(c *polipage.Client) error {
		_, err := c.Documents.Get(context.Background(), "doc_deleted")
		return err
	})
}

func TestErrorCodes_QUOTA_EXCEEDED_429(t *testing.T) {
	t.Parallel()
	expectCode(t, 429, polipage.ErrCodeQuotaExceeded, renderPDF)
}

func TestErrorCodes_OVERAGE_CAP_EXCEEDED_429(t *testing.T) {
	t.Parallel()
	expectCode(t, 429, polipage.ErrCodeOverageCapExceeded, renderPDF)
}

func TestErrorCodes_INVALID_VERSION_FORMAT_400(t *testing.T) {
	t.Parallel()
	expectCode(t, 400, polipage.ErrCodeInvalidVersionFormat, renderPDF)
}

func TestErrorCodes_VERSION_REQUIRED_400(t *testing.T) {
	t.Parallel()
	expectCode(t, 400, polipage.ErrCodeVersionRequired, renderPDF)
}

func TestErrorCodes_INVALID_VERSION_FOR_KEY_ENV_400(t *testing.T) {
	t.Parallel()
	expectCode(t, 400, polipage.ErrCodeInvalidVersionForKeyEnv, renderPDF)
}

func TestErrorCodes_NOT_FOUND_404(t *testing.T) {
	t.Parallel()
	expectCode(t, 404, polipage.ErrCodeNotFound, renderPDF)
}

func TestErrorCodes_VERSION_NOT_FOUND_404(t *testing.T) {
	t.Parallel()
	expectCode(t, 404, polipage.ErrCodeVersionNotFound, renderPDF)
}

func TestErrorCodes_VALIDATION_ERROR_400(t *testing.T) {
	t.Parallel()
	expectCode(t, 400, polipage.ErrCodeValidationError, renderPDF)
}

func TestErrorCodes_MISSING_DATA_400(t *testing.T) {
	t.Parallel()
	expectCode(t, 400, polipage.ErrCodeMissingData, renderPDF)
}

func TestErrorCodes_MISSING_PROJECT_OR_TEMPLATE_400(t *testing.T) {
	t.Parallel()
	expectCode(t, 400, polipage.ErrCodeMissingProjectOrTemplate, renderPDF)
}

func TestErrorCodes_MISSING_TEMPLATE_SLUG_400(t *testing.T) {
	t.Parallel()
	expectCode(t, 400, polipage.ErrCodeMissingTemplateSlug, renderPDF)
}

func TestErrorCodes_MISSING_API_KEY_401(t *testing.T) {
	t.Parallel()
	expectCode(t, 401, polipage.ErrCodeMissingAPIKey, renderPDF)
}

func TestErrorCodes_INVALID_API_KEY_401(t *testing.T) {
	t.Parallel()
	expectCode(t, 401, polipage.ErrCodeInvalidAPIKey, renderPDF)
}

func TestErrorCodes_FORBIDDEN_403(t *testing.T) {
	t.Parallel()
	expectCode(t, 403, polipage.ErrCodeForbidden, renderPDF)
}

func TestErrorCodes_unknownCodePassesThroughVerbatim(t *testing.T) {
	t.Parallel()
	// The SDK must NOT filter the API's wire code — future-compat. A code
	// not in the ErrCode* constant set still round-trips intact.
	expectCode(t, 418, "I_AM_A_TEAPOT", renderPDF)
}
