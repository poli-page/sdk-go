package polipage

import (
	"encoding/json"
	"testing"
)

func TestError_ToPayload_usesAPIStatusForStatusBearingError(t *testing.T) {
	t.Parallel()
	e := &Error{
		Code:       "authentication_failed",
		Message:    "Forbidden",
		StatusCode: 401,
		RequestID:  "req_abc",
	}
	got := e.ToPayload()
	want := Payload{
		Code:      "authentication_failed",
		Message:   "Forbidden",
		Status:    401,
		RequestID: "req_abc",
	}
	if got != want {
		t.Fatalf("ToPayload = %+v, want %+v", got, want)
	}
}

func TestError_ToPayload_usesA503ForNetworkError(t *testing.T) {
	t.Parallel()
	e := &Error{Code: ErrCodeNetworkError, Message: "dns"}
	if got := e.ToPayload().Status; got != 503 {
		t.Fatalf("ToPayload().Status = %d, want 503", got)
	}
	if e.StatusCode != 0 {
		t.Fatalf("e.StatusCode = %d, want 0 (attribute unchanged)", e.StatusCode)
	}
}

func TestError_ToPayload_usesA504ForTimeout(t *testing.T) {
	t.Parallel()
	e := &Error{Code: ErrCodeTimeout, Message: "deadline"}
	if got := e.ToPayload().Status; got != 504 {
		t.Fatalf("ToPayload().Status = %d, want 504", got)
	}
}

func TestError_ToPayload_marshalsCamelCaseRequestId(t *testing.T) {
	t.Parallel()
	e := &Error{Code: "X", Message: "m", StatusCode: 400, RequestID: "req_1"}
	raw, err := json.Marshal(e.ToPayload())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var into map[string]any
	if err := json.Unmarshal(raw, &into); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := into["requestId"]; !ok {
		t.Fatalf("missing requestId key; got %v", into)
	}
	if _, ok := into["request_id"]; ok {
		t.Fatalf("unexpected snake_case request_id key; got %v", into)
	}
}

func TestError_ToPayload_omitsStatusWhenZero(t *testing.T) {
	t.Parallel()
	e := &Error{Code: ErrCodeInvalidOptions, Message: "bad"}
	raw, err := json.Marshal(e.ToPayload())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var into map[string]any
	if err := json.Unmarshal(raw, &into); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := into["status"]; ok {
		t.Fatalf("expected status omitted when zero; got %v", into)
	}
}
