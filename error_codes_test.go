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
		option.WithAPIKey("pp_sandbox_abc"),
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
	_, err := c.Render.PDF(context.Background(), polipage.RenderInput{
		Project: "p", Template: "t",
		Data: map[string]any{},
	})
	return err
}

func documentsGet(c *polipage.Client) error {
	_, err := c.Documents.Get(context.Background(), "doc_1")
	return err
}

func documentsThumbnails(c *polipage.Client) error {
	_, err := c.Documents.Thumbnails(context.Background(), "doc_1", polipage.ThumbnailOptions{})
	return err
}

// TestErrorCodes_knownAPICodesRoundTrip covers every API code of the
// contract (31 in all), each with the status the API answers it with.
func TestErrorCodes_knownAPICodesRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status int
		code   string
		invoke func(*polipage.Client) error
	}{
		{401, polipage.ErrCodeMissingAPIKey, renderPDF},
		{401, polipage.ErrCodeInvalidAPIKey, renderPDF},
		{403, polipage.ErrCodeOrganizationCancelled, renderPDF},
		{410, polipage.ErrCodeOrganizationPurged, renderPDF},
		{403, polipage.ErrCodeForbidden, renderPDF},
		{400, polipage.ErrCodeValidationError, renderPDF},
		{400, polipage.ErrCodeMissingData, renderPDF},
		{400, polipage.ErrCodeMissingTemplateSlug, renderPDF},
		{400, polipage.ErrCodeProjectRequiredForDocument, renderPDF},
		{404, polipage.ErrCodeNotFound, renderPDF},
		{404, polipage.ErrCodeDocumentNotFound, documentsGet},
		{429, polipage.ErrCodeQuotaExceeded, renderPDF},
		{429, polipage.ErrCodeOverageCapExceeded, renderPDF},
		{500, polipage.ErrCodeAPIInternalError, renderPDF},
		{503, polipage.ErrCodePublishInProgress, renderPDF},
		{400, polipage.ErrCodeEnvironmentMismatch, renderPDF},
		{404, polipage.ErrCodeProjectNotInEnvironment, renderPDF},
		{404, polipage.ErrCodeTemplateNotFound, renderPDF},
		{410, polipage.ErrCodeDocumentGone, documentsGet},
		{403, polipage.ErrCodeEnvironmentProtected, renderPDF},
		{403, polipage.ErrCodeOrganizationPaymentGrace, renderPDF},
		{400, polipage.ErrCodeEmptyDocument, renderPDF},
		{502, polipage.ErrCodeStorageAccessDenied, renderPDF},
		{503, polipage.ErrCodeOrganizationMigrating, renderPDF},
		{404, polipage.ErrCodeProjectNotFound, renderPDF},
		{400, polipage.ErrCodeTemplateError, renderPDF},
		{400, polipage.ErrCodeTemplateUnsafe, renderPDF},
		{403, polipage.ErrCodeThumbnailsNotAvailable, documentsThumbnails},
		{429, polipage.ErrCodeRateLimitExceeded, renderPDF},
		{413, polipage.ErrCodeRenderBodyTooLarge, renderPDF},
		{413, polipage.ErrCodeDocumentTooLarge, renderPDF},
		{504, polipage.ErrCodeRenderTimeout, renderPDF},
	}
	if len(cases) != 32 {
		t.Fatalf("len(cases) = %d, want the 32 known API codes", len(cases))
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_%d", tc.code, tc.status), func(t *testing.T) {
			t.Parallel()
			expectCode(t, tc.status, tc.code, tc.invoke)
		})
	}
}

func TestErrorCodes_errorFieldAndDetailFromFlatAPIBody(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"TEMPLATE_NOT_FOUND","detail":"Template 'welcome' not found in project 'getting-started'","requestId":"req_1"}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server)
	err := renderPDF(client)
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		t.Fatalf("err = %v, want *polipage.Error", err)
	}
	if pErr.Code != polipage.ErrCodeTemplateNotFound || pErr.StatusCode != 404 {
		t.Errorf("err = %+v, want Code=TEMPLATE_NOT_FOUND StatusCode=404", pErr)
	}
	if want := "Template 'welcome' not found in project 'getting-started'"; pErr.Message != want {
		t.Errorf("Message = %q, want %q", pErr.Message, want)
	}
}

func TestErrorCodes_unknownCodePassesThroughVerbatim(t *testing.T) {
	t.Parallel()
	// The SDK must NOT filter the API's wire code — future-compat. A code
	// not in the ErrCode* constant set still round-trips intact.
	expectCode(t, 418, "I_AM_A_TEAPOT", renderPDF)
}

func TestErrorCodes_unknownErrorFieldPassesThroughVerbatim(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":"SOME_FUTURE_CODE","detail":"later"}`)
	}))
	t.Cleanup(server.Close)

	err := renderPDF(newTestClient(t, server))
	var pErr *polipage.Error
	if !errors.As(err, &pErr) || pErr.Code != "SOME_FUTURE_CODE" || pErr.Message != "later" {
		t.Fatalf("err = %v, want Code=SOME_FUTURE_CODE Message=later", err)
	}
}
