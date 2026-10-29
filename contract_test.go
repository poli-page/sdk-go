package polipage_test

// Conformance with the Poli Page API contract (https://github.com/poli-page/api-contract).
//
// CI copies the pinned contract.json with the poli-page/api-contract action and sets
// POLI_CONTRACT to its path. Every `kind: "sdk"` case runs against a local HTTP server that
// answers the case's responses in order and records each request; the test then compares the
// recorded requests, the OnRequest / OnRetry events and the normalized result with the case's
// `expect`. Timing is checked as lower bounds, measured on the server.
//
// tests/contract-skip.json maps the case ids allowed to fail to the reason. A listed case that
// passes, or a listed id the contract does not have, fails the run.
//
// Locally: `POLI_CONTRACT=/path/to/contract.json go test -run TestContract .`. Without it the
// test is skipped, except when CI is set, where a missing contract is an error.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

// contractFormatVersion is the contract.json layout this file reads (README of
// poli-page/api-contract).
const contractFormatVersion = 1

// contractSDKName is this SDK's name in policy.cancellationSdks.
const contractSDKName = "go"

// contractTestAPIKey is the client's key when a case sets no apiKey.
const contractTestAPIKey = "pp_sandbox_00000000000000000000000000000000"

// contractSkipFile lists the case ids allowed to fail, with the reason.
const contractSkipFile = "tests/contract-skip.json"

// contractCaseTimeout bounds one case; the slowest (a 2 s Retry-After date) takes about 2 s.
const contractCaseTimeout = 30 * time.Second

// contractNormalizations are the policy.normalizations ids this file applies; each must still
// be listed by the contract.
var contractNormalizations = []string{
	"N-STATUS-NULL",
	"N-REQID-NULL",
	"N-BYTES",
	"N-DELETE-NULL",
	"N-THUMBS-ARRAY",
	"N-PREVIEW",
	"N-DESCRIPTOR",
	"N-DURATION-MS",
}

// descriptorFields are the DocumentDescriptor fields of sdk-specification.md §7.1
// (N-DESCRIPTOR).
var descriptorFields = []string{
	"documentId", "organizationId", "projectId", "projectSlug", "templateId", "templateSlug",
	"environment", "apiKeyId", "format", "orientation", "locale", "pageCount", "sizeBytes",
	"createdAt", "metadata", "presignedPdfUrl", "expiresAt",
}

var (
	placeholderPattern = regexp.MustCompile(`\{\{([^}]*)\}\}`)
	httpDatePattern    = regexp.MustCompile(`^httpDate\+(\d+)$`)
	uuidV4Pattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// ---------------------------------------------------------------------------
// Contract format 1
// ---------------------------------------------------------------------------

type contractFile struct {
	FormatVersion int               `json:"formatVersion"`
	Policy        contractPolicy    `json:"policy"`
	Cases         []json.RawMessage `json:"cases"`
}

type contractPolicy struct {
	CancellationSdks []string `json:"cancellationSdks"`
	Normalizations   []struct {
		ID string `json:"id"`
	} `json:"normalizations"`
}

type caseHeader struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type sdkCase struct {
	Kind     string  `json:"kind"`
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Requires string  `json:"requires"`
	APIKey   *string `json:"apiKey"`
	Options  struct {
		MaxRetries   int `json:"maxRetries"`
		RetryDelayMs int `json:"retryDelayMs"`
		TimeoutMs    int `json:"timeoutMs"`
	} `json:"options"`
	Call struct {
		Method        string            `json:"method"`
		Args          []json.RawMessage `json:"args"`
		CancelAfterMs *int              `json:"cancelAfterMs"`
	} `json:"call"`
	Responses []mockStep `json:"responses"`
	Expect    struct {
		Requests []map[string]any `json:"requests"`
		Events   []map[string]any `json:"events"`
		Result   any              `json:"result"`
	} `json:"expect"`
	StopAfterRetryEvents *int `json:"stopAfterRetryEvents"`
}

type mockStep struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	JSON    json.RawMessage   `json:"json"`
	Text    *string           `json:"text"`
	Base64  *string           `json:"base64"`
	Fault   string            `json:"fault"`
}

func loadContract(path string) (*contractFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		FormatVersion *int `json:"formatVersion"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("%s is not JSON: %w", path, err)
	}
	if probe.FormatVersion == nil {
		return nil, fmt.Errorf("%s is not a contract.json: it has no formatVersion", path)
	}
	if *probe.FormatVersion != contractFormatVersion {
		return nil, fmt.Errorf("contract.json formatVersion %d is not supported (this test reads format %d): update contract_test.go",
			*probe.FormatVersion, contractFormatVersion)
	}
	var c contractFile
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// decodeSdkCase decodes one case strictly: a field this file does not know fails the case
// instead of being ignored.
func decodeSdkCase(raw json.RawMessage) (*sdkCase, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c sdkCase
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	if c.Requires != "" && c.Requires != "cancellation" {
		return nil, fmt.Errorf("unknown case requirement %q", c.Requires)
	}
	for i, step := range c.Responses {
		bodies := 0
		if step.JSON != nil {
			bodies++
		}
		if step.Text != nil {
			bodies++
		}
		if step.Base64 != nil {
			bodies++
		}
		switch {
		case step.Fault != "" && step.Fault != "drop" && step.Fault != "hang":
			return nil, fmt.Errorf("responses[%d]: unknown fault %q", i, step.Fault)
		case step.Fault != "" && (bodies > 0 || step.Status != 0 || step.Headers != nil):
			return nil, fmt.Errorf("responses[%d]: a fault has no status, headers or body", i)
		case step.Fault == "" && step.Status == 0:
			return nil, fmt.Errorf("responses[%d]: no status", i)
		case bodies > 1:
			return nil, fmt.Errorf("responses[%d]: more than one of json, text, base64", i)
		}
		if step.Base64 != nil {
			if _, err := base64.StdEncoding.DecodeString(*step.Base64); err != nil {
				return nil, fmt.Errorf("responses[%d].base64: %w", i, err)
			}
		}
	}
	return &c, nil
}

func loadSkipList() (map[string]string, error) {
	raw, err := os.ReadFile(contractSkipFile)
	if err != nil {
		return nil, err
	}
	var entries map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		return nil, fmt.Errorf("%s must be an object of case id to reason", contractSkipFile)
	}
	skip := make(map[string]string, len(entries))
	for id, reason := range entries {
		text, ok := reason.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("%s: %q needs a reason", contractSkipFile, id)
		}
		skip[id] = text
	}
	return skip, nil
}

// ---------------------------------------------------------------------------
// Placeholders: {{mock}}, {{httpDate+N}}
// ---------------------------------------------------------------------------

func substituteString(s, mock string) (string, error) {
	var bad error
	out := placeholderPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-2]
		if name == "mock" {
			return mock
		}
		if m := httpDatePattern.FindStringSubmatch(name); m != nil {
			secs, err := strconv.Atoi(m[1])
			if err != nil {
				bad = fmt.Errorf("placeholder %s: %w", match, err)
				return match
			}
			// An HTTP-date has whole seconds: round to the nearest one.
			at := time.Now().Add(time.Duration(secs) * time.Second).Round(time.Second)
			return at.UTC().Format(http.TimeFormat)
		}
		bad = fmt.Errorf("unknown contract placeholder %s", match)
		return match
	})
	return out, bad
}

// substitute replaces the placeholders of every string in a decoded JSON value.
func substitute(v any, mock string) (any, error) {
	switch x := v.(type) {
	case string:
		return substituteString(x, mock)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			s, err := substitute(item, mock)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			s, err := substitute(item, mock)
			if err != nil {
				return nil, err
			}
			out[k] = s
		}
		return out, nil
	default:
		return v, nil
	}
}

// ---------------------------------------------------------------------------
// Mock server
// ---------------------------------------------------------------------------

type recordedRequest struct {
	method string
	path   string
	header http.Header
	body   []byte
	at     time.Time // monotonic arrival time
}

type mockServer struct {
	srv   *httptest.Server
	steps []mockStep
	done  chan struct{}

	mu       sync.Mutex
	requests []recordedRequest
	problems []string
}

func startMockServer(steps []mockStep) *mockServer {
	m := &mockServer{steps: steps, done: make(chan struct{})}
	m.srv = httptest.NewUnstartedServer(http.HandlerFunc(m.serve))
	// One connection per request. Here the API and the storage share one origin, so the
	// download would reuse the render's keep-alive connection, and net/http's Transport resends
	// a replayable request (a GET, or a POST with Idempotency-Key) once when a reused
	// connection dies before any answer: the server would see a request the SDK never made.
	m.srv.Config.SetKeepAlivesEnabled(false)
	m.srv.Start()
	return m
}

func (m *mockServer) origin() string { return m.srv.URL }

func (m *mockServer) recorded() []recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.requests)
}

func (m *mockServer) serverProblems() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.problems)
}

func (m *mockServer) close() {
	close(m.done)
	m.srv.CloseClientConnections()
	m.srv.Close()
}

func (m *mockServer) serve(w http.ResponseWriter, r *http.Request) {
	at := time.Now()
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	m.requests = append(m.requests, recordedRequest{
		method: r.Method,
		path:   r.RequestURI,
		header: r.Header.Clone(),
		body:   body,
		at:     at,
	})
	index := len(m.requests) - 1
	m.mu.Unlock()

	if index >= len(m.steps) {
		// More requests than the case has answers: the request list shows it.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(599)
		_, _ = w.Write([]byte(`{"error":"MOCK_EXHAUSTED"}`))
		return
	}
	step := m.steps[index]
	switch step.Fault {
	case "drop":
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		m.problem("cannot drop the connection of request %d", index+1)
	case "hang":
		select {
		case <-r.Context().Done():
		case <-m.done:
		}
	default:
		if err := m.answer(w, step); err != nil {
			m.problem("answer %d: %v", index+1, err)
		}
	}
}

func (m *mockServer) problem(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.problems = append(m.problems, fmt.Sprintf(format, args...))
}

func (m *mockServer) answer(w http.ResponseWriter, step mockStep) error {
	origin := m.origin()
	for name, value := range step.Headers {
		v, err := substituteString(value, origin)
		if err != nil {
			return err
		}
		w.Header().Set(name, v)
	}
	var body []byte
	switch {
	case step.JSON != nil:
		var decoded any
		if err := json.Unmarshal(step.JSON, &decoded); err != nil {
			return err
		}
		substituted, err := substitute(decoded, origin)
		if err != nil {
			return err
		}
		if body, err = json.Marshal(substituted); err != nil {
			return err
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
	case step.Text != nil:
		body = []byte(*step.Text)
	case step.Base64 != nil:
		body, _ = base64.StdEncoding.DecodeString(*step.Base64)
	}
	w.WriteHeader(step.Status)
	_, err := w.Write(body)
	return err
}

// ---------------------------------------------------------------------------
// Calling the SDK
// ---------------------------------------------------------------------------

func decodeArg(args []json.RawMessage, index int, into any) error {
	dec := json.NewDecoder(bytes.NewReader(args[index]))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("call.args[%d]: %w", index, err)
	}
	return nil
}

func argCount(args []json.RawMessage, minArgs, maxArgs int) error {
	if len(args) < minArgs || len(args) > maxArgs {
		return fmt.Errorf("call.args has %d values, want %d to %d", len(args), minArgs, maxArgs)
	}
	return nil
}

// invoke calls method with args. callErr is the SDK's error; setupErr a case the test cannot run.
func invoke(ctx context.Context, client *polipage.Client, method string, args []json.RawMessage) (value any, callErr, setupErr error) {
	switch method {
	case "render.pdf", "render.pdfStream", "render.document":
		if err := argCount(args, 1, 1); err != nil {
			return nil, nil, err
		}
		var in polipage.RenderInput
		if err := decodeArg(args, 0, &in); err != nil {
			return nil, nil, err
		}
		switch method {
		case "render.pdf":
			pdf, err := client.Render.PDF(ctx, in)
			if err != nil {
				return nil, err, nil
			}
			return normalizeBytes(pdf), nil, nil
		case "render.pdfStream":
			stream, err := client.Render.PDFStream(ctx, in)
			if err != nil {
				return nil, err, nil
			}
			defer func() { _ = stream.Close() }()
			pdf, err := io.ReadAll(stream)
			if err != nil {
				return nil, err, nil
			}
			return normalizeBytes(pdf), nil, nil
		default:
			doc, err := client.Render.Document(ctx, in)
			if err != nil {
				return nil, err, nil
			}
			v, err := normalizeDescriptor(doc)
			return v, nil, err
		}
	case "documents.get", "documents.preview", "documents.delete":
		if err := argCount(args, 1, 1); err != nil {
			return nil, nil, err
		}
		var id string
		if err := decodeArg(args, 0, &id); err != nil {
			return nil, nil, err
		}
		switch method {
		case "documents.get":
			doc, err := client.Documents.Get(ctx, id)
			if err != nil {
				return nil, err, nil
			}
			v, err := normalizeDescriptor(doc)
			return v, nil, err
		case "documents.preview":
			preview, err := client.Documents.Preview(ctx, id)
			if err != nil {
				return nil, err, nil
			}
			// N-PREVIEW
			return map[string]any{"html": preview.HTML, "pageCount": float64(preview.PageCount)}, nil, nil
		default:
			if err := client.Documents.Delete(ctx, id); err != nil {
				return nil, err, nil
			}
			return nil, nil, nil // N-DELETE-NULL
		}
	case "documents.thumbnails":
		if err := argCount(args, 1, 2); err != nil {
			return nil, nil, err
		}
		var id string
		if err := decodeArg(args, 0, &id); err != nil {
			return nil, nil, err
		}
		var options polipage.ThumbnailOptions
		if len(args) > 1 {
			if err := decodeArg(args, 1, &options); err != nil {
				return nil, nil, err
			}
		}
		thumbs, err := client.Documents.Thumbnails(ctx, id, options)
		if err != nil {
			return nil, err, nil
		}
		// N-THUMBS-ARRAY: the SDK already returns the unwrapped slice.
		v, err := jsonValue(thumbs)
		return v, nil, err
	default:
		return nil, nil, fmt.Errorf("unknown SDK method in the contract: %s", method)
	}
}

// jsonValue turns v into its decoded-JSON form, the form the contract's values are read in.
func jsonValue(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// normalizeBytes is N-BYTES.
func normalizeBytes(data []byte) any {
	sum := sha256.Sum256(data)
	return map[string]any{"sha256": hex.EncodeToString(sum[:]), "length": float64(len(data))}
}

// normalizeDescriptor is N-DESCRIPTOR.
func normalizeDescriptor(doc *polipage.DocumentDescriptor) (any, error) {
	v, err := jsonValue(doc)
	if err != nil {
		return nil, err
	}
	fields, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("descriptor is not a JSON object: %v", v)
	}
	out := make(map[string]any, len(descriptorFields))
	for _, name := range descriptorFields {
		if value, ok := fields[name]; ok {
			out[name] = value
		}
	}
	return out, nil
}

// normalizeError is N-STATUS-NULL and N-REQID-NULL. An error that is not a *polipage.Error
// shows up in the diff.
func normalizeError(err error) map[string]any {
	var pErr *polipage.Error
	if !errors.As(err, &pErr) {
		return map[string]any{
			"code": "not a *polipage.Error", "status": nil,
			"message": fmt.Sprintf("%T: %v", err, err), "requestId": nil,
			"retryable": false, "isAuthError": false, "isRateLimitError": false,
			"isValidationError": false, "isNetworkError": false,
		}
	}
	var status, requestID any
	if pErr.StatusCode != 0 {
		status = float64(pErr.StatusCode)
	}
	if pErr.RequestID != "" {
		requestID = pErr.RequestID
	}
	return map[string]any{
		"code":              pErr.Code,
		"status":            status,
		"message":           pErr.Message,
		"requestId":         requestID,
		"retryable":         pErr.IsRetryable(),
		"isAuthError":       pErr.IsAuthError(),
		"isRateLimitError":  pErr.IsRateLimitError(),
		"isValidationError": pErr.IsValidationError(),
		"isNetworkError":    pErr.IsNetworkError(),
	}
}

// ---------------------------------------------------------------------------
// Running and comparing one case
// ---------------------------------------------------------------------------

type observedEvent struct {
	kind    string
	attempt int
	delayMs float64
}

// describeRequests returns the recorded requests in the shape of expect.requests. A field that
// breaks the expectation keeps the observed value, so the diff shows it.
func describeRequests(recorded []recordedRequest, expected []map[string]any, apiKey string) []any {
	out := make([]any, 0, len(recorded))
	firstKey := ""
	for i, req := range recorded {
		var want map[string]any
		if i < len(expected) {
			want = expected[i]
		}
		auth := "none"
		if values, ok := req.header["Authorization"]; ok {
			auth = "unexpected Authorization: " + strings.Join(values, ", ")
			if len(values) == 1 && values[0] == "Bearer "+apiKey {
				auth = "bearer"
			}
		}
		idempotencyKey := "absent"
		if values, ok := req.header["Idempotency-Key"]; ok {
			key := strings.Join(values, ", ")
			switch {
			case !uuidV4Pattern.MatchString(key):
				idempotencyKey = "not a lowercase UUID v4: " + key
			case firstKey == "" || key == firstKey:
				firstKey = key
				idempotencyKey = "stable"
			default:
				idempotencyKey = "changed: " + firstKey + " then " + key
			}
		}
		item := map[string]any{
			"method":         req.method,
			"path":           req.path,
			"auth":           auth,
			"idempotencyKey": idempotencyKey,
		}
		if _, ok := want["json"]; ok {
			var body any
			if err := json.Unmarshal(req.body, &body); err != nil {
				item["json"] = "body is not JSON: " + string(req.body)
			} else {
				item["json"] = body
			}
		}
		if minGap, ok := want["minGapMsBefore"].(float64); ok {
			gap := 0.0
			if i > 0 {
				// N-DURATION-MS: the gap is compared in integer milliseconds.
				gap = math.Round(float64(req.at.Sub(recorded[i-1].at)) / float64(time.Millisecond))
			}
			if gap >= minGap {
				item["minGapMsBefore"] = minGap
			} else {
				item["minGapMsBefore"] = fmt.Sprintf("only %.0f ms", gap)
			}
		}
		out = append(out, item)
	}
	return out
}

// describeEvents returns the observed events, a delay inside an expected range shown as that
// range.
func describeEvents(observed []observedEvent, expected []map[string]any) []any {
	out := make([]any, 0, len(observed))
	for i, event := range observed {
		item := map[string]any{"type": event.kind, "attempt": float64(event.attempt)}
		if event.kind == "retry" {
			// N-DURATION-MS
			delay := math.Round(event.delayMs)
			item["delayMs"] = delay
			if i < len(expected) {
				if want, ok := expected[i]["delayMs"].(map[string]any); ok {
					lo, loOK := want["min"].(float64)
					hi, hiOK := want["max"].(float64)
					if loOK && hiOK && delay >= lo && delay < hi {
						item["delayMs"] = want
					}
				}
			}
		}
		out = append(out, item)
	}
	return out
}

// expectedPrefix cuts the expectations at the stopAfterRetryEvents-th retry event, when the
// case sets one.
func expectedPrefix(c *sdkCase) (events, requests []map[string]any, checkResult bool, err error) {
	if c.StopAfterRetryEvents == nil {
		return c.Expect.Events, c.Expect.Requests, true, nil
	}
	retries := 0
	for i, event := range c.Expect.Events {
		if event["type"] == "retry" {
			retries++
			if retries == *c.StopAfterRetryEvents {
				events = c.Expect.Events[:i+1]
				attempts := 0
				for _, e := range events {
					if e["type"] == "request" {
						attempts++
					}
				}
				if attempts > len(c.Expect.Requests) {
					return nil, nil, false, fmt.Errorf("stopAfterRetryEvents %d: more request events than requests", *c.StopAfterRetryEvents)
				}
				return events, c.Expect.Requests[:attempts], false, nil
			}
		}
	}
	return nil, nil, false, fmt.Errorf("stopAfterRetryEvents %d: the case has fewer retries", *c.StopAfterRetryEvents)
}

func toAnySlice(items []map[string]any) []any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}

func compare(label string, got, want any) string {
	if reflect.DeepEqual(got, want) {
		return ""
	}
	gotJSON, _ := json.MarshalIndent(got, "  ", "  ")
	wantJSON, _ := json.MarshalIndent(want, "  ", "  ")
	return fmt.Sprintf("%s differ\n  want: %s\n  got:  %s", label, wantJSON, gotJSON)
}

// runCase runs one case and returns what did not match; empty means the case passes.
func runCase(c *sdkCase) []string {
	server := startMockServer(c.Responses)
	apiKey := contractTestAPIKey
	if c.APIKey != nil {
		apiKey = *c.APIKey
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var events []observedEvent
	retries := 0
	client := polipage.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(server.origin()),
		option.WithMaxRetries(c.Options.MaxRetries),
		option.WithRetryDelay(time.Duration(c.Options.RetryDelayMs)*time.Millisecond),
		option.WithTimeout(time.Duration(c.Options.TimeoutMs)*time.Millisecond),
		option.WithOnRequest(func(e polipage.RequestEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, observedEvent{kind: "request", attempt: e.Attempt})
		}),
		option.WithOnRetry(func(e polipage.RetryEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, observedEvent{kind: "retry", attempt: e.Attempt, delayMs: e.DelayMs})
			retries++
			if c.StopAfterRetryEvents != nil && retries == *c.StopAfterRetryEvents {
				cancel()
			}
		}),
	)
	if c.Call.CancelAfterMs != nil {
		timer := time.AfterFunc(time.Duration(*c.Call.CancelAfterMs)*time.Millisecond, cancel)
		defer timer.Stop()
	}

	type outcome struct {
		value             any
		callErr, setupErr error
	}
	finished := make(chan outcome, 1)
	go func() {
		value, callErr, setupErr := invoke(ctx, client, c.Call.Method, c.Call.Args)
		finished <- outcome{value, callErr, setupErr}
	}()
	var got outcome
	select {
	case got = <-finished:
	case <-time.After(contractCaseTimeout):
		cancel()
		server.close()
		return []string{fmt.Sprintf("the call did not return within %v", contractCaseTimeout)}
	}
	server.close()
	if got.setupErr != nil {
		return []string{"cannot run the case: " + got.setupErr.Error()}
	}

	problems := server.serverProblems()
	wantEvents, wantRequests, checkResult, err := expectedPrefix(c)
	if err != nil {
		return append(problems, err.Error())
	}
	origin := server.origin()
	substitutedRequests, err := substitute(toAnySlice(wantRequests), origin)
	if err != nil {
		return append(problems, err.Error())
	}
	if diff := compare("requests", describeRequests(server.recorded(), wantRequests, apiKey), substitutedRequests); diff != "" {
		problems = append(problems, diff)
	}
	mu.Lock()
	observed := slices.Clone(events)
	mu.Unlock()
	if diff := compare("OnRequest / OnRetry events", describeEvents(observed, wantEvents), toAnySlice(wantEvents)); diff != "" {
		problems = append(problems, diff)
	}
	if checkResult {
		wantResult, err := substitute(c.Expect.Result, origin)
		if err != nil {
			return append(problems, err.Error())
		}
		var gotResult any
		if got.callErr != nil {
			gotResult = map[string]any{"ok": false, "error": normalizeError(got.callErr)}
		} else {
			gotResult = map[string]any{"ok": true, "value": got.value}
		}
		if diff := compare("result", gotResult, wantResult); diff != "" {
			problems = append(problems, diff)
		}
	}
	return problems
}

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

func TestContract(t *testing.T) {
	path := os.Getenv("POLI_CONTRACT")
	if path == "" {
		if os.Getenv("CI") != "" {
			t.Fatal(`POLI_CONTRACT is not set: in CI the "API contract" step must provide contract.json (poli-page/api-contract action, output "path")`)
		}
		t.Skip("API contract tests skipped: set POLI_CONTRACT to a contract.json path")
	}
	contract, err := loadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	skip, err := loadSkipList()
	if err != nil {
		t.Fatal(err)
	}

	var cases []*sdkCase
	var caseErrors []string
	ids := map[string]bool{}
	for i, raw := range contract.Cases {
		var head caseHeader
		if err := json.Unmarshal(raw, &head); err != nil {
			t.Fatalf("cases[%d]: %v", i, err)
		}
		switch head.Kind {
		case "sdk":
			ids[head.ID] = true
			c, err := decodeSdkCase(raw)
			if err != nil {
				caseErrors = append(caseErrors, fmt.Sprintf("%s: %v", head.ID, err))
				continue
			}
			cases = append(cases, c)
		case "impl":
			// For integration libraries.
		default:
			t.Errorf("cases[%d] (%s) has kind %q: this test knows only \"sdk\" and \"impl\"", i, head.ID, head.Kind)
		}
	}
	if len(ids) == 0 {
		t.Fatal(`the contract has no "sdk" case`)
	}
	for _, msg := range caseErrors {
		t.Errorf("cannot read case %s", msg)
	}

	allowed := map[string]bool{}
	for _, n := range contract.Policy.Normalizations {
		allowed[n.ID] = true
	}
	for _, id := range contractNormalizations {
		if !allowed[id] {
			t.Errorf("normalization %s is applied here but no longer listed by the contract", id)
		}
	}
	for id := range skip {
		if !ids[id] {
			t.Errorf("%s lists %q, which the contract does not have: remove it from %s", contractSkipFile, id, contractSkipFile)
		}
	}
	runsCancellation := slices.Contains(contract.Policy.CancellationSdks, contractSDKName)

	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			t.Parallel()
			if c.Requires == "cancellation" && !runsCancellation {
				t.Skipf("policy.cancellationSdks does not list %q", contractSDKName)
			}
			problems := runCase(c)
			reason, listed := skip[c.ID]
			switch {
			case !listed && len(problems) > 0:
				t.Errorf("%s\n%s", c.Title, strings.Join(problems, "\n"))
			case listed && len(problems) == 0:
				t.Errorf("%s passes now: remove it from %s", c.ID, contractSkipFile)
			case listed:
				t.Skipf("expected to fail (%s): %s", reason, strings.Join(problems, "\n"))
			}
		})
	}
}
