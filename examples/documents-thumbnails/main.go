// Demonstrates: client.Documents.Thumbnails(ctx, id, options) — page thumbnails for a stored document.
package main

import (
	"context"
	"fmt"
	"os"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

func main() {
	client := polipage.NewClient(
		option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
	)

	thumbnails, err := client.Documents.Thumbnails(
		context.Background(),
		"doc_abc123",
		polipage.ThumbnailOptions{
			Width:  840,
			Format: polipage.ThumbnailFormatPNG,
			Pages:  []int{1, 2},
		},
	)
	if err != nil {
		panic(err)
	}

	// Each entry includes the image bytes base64-encoded.
	for _, t := range thumbnails {
		fmt.Printf("Page %d: %dx%d %s (%d base64 chars)\n", t.Page, t.Width, t.Height, t.ContentType, len(t.Data))
	}
}
