//go:build integration

package polipage_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	polipage "github.com/poli-page/sdk-go"
)

// TestIntegration_Documents_roundTrip drives the full stored-document
// lifecycle against the develop API:
//
//	Render.Document → Documents.Get → DownloadPDF → Documents.Thumbnails
//	→ Documents.Preview → Documents.Delete (twice) → Documents.Get
//	returns DOCUMENT_GONE.
//
// Tier-gated thumbnails (Free tier returns THUMBNAILS_NOT_AVAILABLE) are
// tolerated — wire-level assertions run only when the call succeeds.
func TestIntegration_Documents_roundTrip(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()

	in := integrationInput()
	in.Data = map[string]any{"id": time.Now().UnixNano()}
	in.Metadata = polipage.RenderMetadata{"source": "sdk-go documents integration test"}
	created, err := client.Render.Document(ctx, in)
	if err != nil {
		t.Fatalf("Render.Document err = %v", err)
	}
	if created.DocumentID == "" {
		t.Fatal("DocumentID empty")
	}

	// 2. Documents.Get fetches a fresh descriptor.
	fetched, err := client.Documents.Get(ctx, created.DocumentID)
	if err != nil {
		t.Fatalf("Documents.Get err = %v", err)
	}
	if fetched.DocumentID != created.DocumentID {
		t.Errorf("fetched.DocumentID = %q, want %q", fetched.DocumentID, created.DocumentID)
	}
	if got, _ := fetched.Metadata["source"].(string); got != "sdk-go documents integration test" {
		t.Errorf("fetched.Metadata[source] = %q, want 'sdk-go documents integration test'", got)
	}

	// 3. Back-ref works: DownloadPDF on the descriptor from Get.
	pdf, err := fetched.DownloadPDF(ctx)
	if err != nil {
		t.Fatalf("DownloadPDF err = %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Errorf("PDF prefix = %q, want %%PDF", string(pdf[:4]))
	}

	// 4. Documents.Thumbnails. Free tier may return THUMBNAILS_NOT_AVAILABLE
	// — tolerate it rather than fail the whole round trip.
	thumbs, err := client.Documents.Thumbnails(ctx, created.DocumentID, polipage.ThumbnailOptions{
		Width:  320,
		Format: polipage.ThumbnailFormatPNG,
	})
	if err != nil {
		var pErr *polipage.Error
		if !errors.As(err, &pErr) || pErr.Code != "THUMBNAILS_NOT_AVAILABLE" {
			t.Fatalf("Thumbnails err = %v", err)
		}
		t.Log("skipping thumbnail assertions: tier returned THUMBNAILS_NOT_AVAILABLE")
	} else {
		if len(thumbs) == 0 {
			t.Error("Thumbnails returned empty slice")
		}
		if len(thumbs) > 0 && thumbs[0].ContentType != "image/png" {
			t.Errorf("thumbs[0].ContentType = %q, want image/png", thumbs[0].ContentType)
		}
	}
	if err == nil {
		// No option at all: the API applies its defaults (400 px JPEG).
		if _, err := client.Documents.Thumbnails(ctx, created.DocumentID, polipage.ThumbnailOptions{}); err != nil {
			t.Fatalf("Thumbnails with no options err = %v", err)
		}
	}

	// 5. Documents.Preview returns HTML + pageCount.
	preview, err := client.Documents.Preview(ctx, created.DocumentID)
	if err != nil {
		t.Fatalf("Documents.Preview err = %v", err)
	}
	if preview.HTML == "" {
		t.Error("Preview returned empty HTML")
	}
	if preview.PageCount <= 0 {
		t.Errorf("PageCount = %d, want > 0", preview.PageCount)
	}

	// 6. Soft-delete; deleting again also succeeds (idempotent 204).
	if err := client.Documents.Delete(ctx, created.DocumentID); err != nil {
		t.Fatalf("Documents.Delete err = %v", err)
	}
	if err := client.Documents.Delete(ctx, created.DocumentID); err != nil {
		t.Fatalf("second Documents.Delete err = %v, want nil", err)
	}

	// 7. Subsequent Get returns 410 DOCUMENT_GONE.
	_, err = client.Documents.Get(ctx, created.DocumentID)
	if !errors.Is(err, polipage.ErrDocumentGone) {
		t.Fatalf("err = %v, want errors.Is(..., ErrDocumentGone)", err)
	}
}
