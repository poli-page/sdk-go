package constants

import "testing"

func TestPathDocument_escapesID(t *testing.T) {
	t.Parallel()
	got := PathDocument("doc with space")
	want := "/v1/documents/doc%20with%20space"
	if got != want {
		t.Fatalf("PathDocument = %q, want %q", got, want)
	}
}

func TestPathDocumentPreview_escapesID(t *testing.T) {
	t.Parallel()
	got := PathDocumentPreview("doc/slash")
	want := "/v1/documents/doc%2Fslash/preview"
	if got != want {
		t.Fatalf("PathDocumentPreview = %q, want %q", got, want)
	}
}

func TestPathDocumentThumbnails_escapesID(t *testing.T) {
	t.Parallel()
	got := PathDocumentThumbnails("doc#hash")
	want := "/v1/documents/doc%23hash/thumbnails"
	if got != want {
		t.Fatalf("PathDocumentThumbnails = %q, want %q", got, want)
	}
}

func TestPathRender_constants(t *testing.T) {
	t.Parallel()
	if PathRender != "/v1/render" {
		t.Errorf("PathRender = %q, want /v1/render", PathRender)
	}
	if PathRenderPreview != "/v1/render/preview" {
		t.Errorf("PathRenderPreview = %q, want /v1/render/preview", PathRenderPreview)
	}
}

func TestHeader_namesAreCanonical(t *testing.T) {
	t.Parallel()
	pairs := map[string]string{
		HeaderAuthorization:     "Authorization",
		HeaderContentType:       "Content-Type",
		HeaderAccept:            "Accept",
		HeaderUserAgent:         "User-Agent",
		HeaderIdempotencyKey:    "Idempotency-Key",
		HeaderRetryAfter:        "Retry-After",
		HeaderRequestID:         "X-Request-Id",
		HeaderDocumentPageCount: "X-Document-Page-Count",
	}
	for got, want := range pairs {
		if got != want {
			t.Errorf("header constant = %q, want canonical %q", got, want)
		}
	}
}
