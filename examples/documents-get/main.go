// Demonstrates: client.Documents.Get(ctx, id) — fetch a stored document.
package main

import (
	"context"
	"fmt"
	"os"

	polipage "github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

func main() {
	ctx := context.Background()
	client := polipage.NewClient(
		option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
	)

	doc, err := client.Documents.Get(ctx, "doc_abc123")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Document %s: %d pages, created %s\n", doc.DocumentID, doc.PageCount, doc.CreatedAt)

	// PresignedPDFURL has a 15-minute TTL. Call DownloadPDF to fetch bytes
	// before it expires, or call Documents.Get again to refresh.
	pdf, err := doc.DownloadPDF(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Downloaded %d bytes\n", len(pdf))
}
