package polipage

import "testing"

// Compile-time assertions that the sealed RenderInput interface is
// satisfied by ProjectModeInput and InlineModeInput. External packages
// CANNOT satisfy this interface — the marker method is unexported.
//
// The following call patterns MUST fail to compile (verify by hand if
// ever in doubt; there is no automated negative-compile harness):
//
//	client.Render.PDF(ctx, polipage.InlineModeInput{})      // wrong input type
//	client.Render.PDFStream(ctx, polipage.InlineModeInput{}) // wrong input type
//	client.Render.Document(ctx, polipage.InlineModeInput{}) // wrong input type
var (
	_ RenderInput = ProjectModeInput{}
	_ RenderInput = InlineModeInput{}
)

func TestOpt_returnsPointerToValue(t *testing.T) {
	t.Parallel()
	p := Opt("hello")
	if p == nil || *p != "hello" {
		t.Fatalf("Opt(\"hello\") = %v, want pointer to \"hello\"", p)
	}
}

func TestTypeShape_RequestEvent(t *testing.T) {
	t.Parallel()
	var e RequestEvent
	e.Method = "POST"
	e.URL = "https://api.poli.page/v1/render"
	e.Attempt = 1
	// Compiles iff the three fields exist with the documented types.
	_ = e
}

func TestTypeShape_ResponseEvent(t *testing.T) {
	t.Parallel()
	var e ResponseEvent
	e.Status = 200
	e.RequestID = "req_abc"
	e.DurationMs = int64(42)
	_ = e
}
