// Demonstrates: client.Render.Document(ctx, in) — render and store a PDF server-side.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

func main() {
	ctx := context.Background()
	client := polipage.NewClient(
		option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
	)

	doc, err := client.Render.Document(ctx, polipage.ProjectModeInput{
		Project:  "billing",
		Template: "invoice",
		Data:     map[string]any{"invoiceNumber": "INV-001", "total": 1280},
		Metadata: polipage.RenderMetadata{"customerId": "cust_42"},
	})
	if err != nil {
		panic(err)
	}

	// doc.DocumentID identifies the stored document — use it with
	// client.Documents.* to fetch, preview, thumbnail, or delete later.
	fmt.Printf("Stored as %s (%d pages, %d bytes)\n", doc.DocumentID, doc.PageCount, doc.SizeBytes)

	// Fetch the PDF bytes on demand:
	pdf, err := doc.DownloadPDF(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Downloaded %d bytes\n", len(pdf))
}
