// Demonstrates: client.Render.PDFStream(ctx, in) — project mode only.
package main

import (
	"context"
	"io"
	"os"

	"github.com/poli-page/sdk-go"
	"github.com/poli-page/sdk-go/option"
)

func main() {
	client := polipage.NewClient(
		option.WithAPIKey(os.Getenv("POLI_PAGE_API_KEY")),
	)

	body, err := client.Render.PDFStream(context.Background(), polipage.ProjectModeInput{
		Project:  "billing",
		Template: "invoice",
		Data:     map[string]any{"invoiceNumber": "INV-001", "total": 1280},
	})
	if err != nil {
		panic(err)
	}
	defer body.Close()

	// Stream directly to an HTTP response writer, an S3 multipart upload, or any
	// io.Writer — bounded memory regardless of PDF size.
	n, err := io.Copy(io.Discard, body)
	if err != nil {
		panic(err)
	}
	_ = n
}
