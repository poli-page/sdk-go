// Demonstrates: client.Documents.Preview(ctx, id) — get a stored document's HTML preview.
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

	preview, err := client.Documents.Preview(context.Background(), "doc_abc123")
	if err != nil {
		panic(err)
	}

	// preview.HTML is the server-rendered HTML with the stored document's data
	// applied to its template — useful for in-browser previews without a PDF.
	fmt.Printf("Preview: %d pages, HTML length %d\n", preview.PageCount, len(preview.HTML))
}
