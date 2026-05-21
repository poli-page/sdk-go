package polipage

import (
	"github.com/poli-page/sdk-go/internal/clientconfig"
)

// PageFormat is a canonical Poli Page page format. Values match the
// strings accepted by the deployed API and shared across every SDK.
type PageFormat string

// Canonical page formats. See the platform spec for the authoritative list.
const (
	PageFormatA3        PageFormat = "A3"
	PageFormatA4        PageFormat = "A4"
	PageFormatA5        PageFormat = "A5"
	PageFormatA6        PageFormat = "A6"
	PageFormatB4        PageFormat = "B4"
	PageFormatB5        PageFormat = "B5"
	PageFormatLetter    PageFormat = "Letter"
	PageFormatLegal     PageFormat = "Legal"
	PageFormatTabloid   PageFormat = "Tabloid"
	PageFormatExecutive PageFormat = "Executive"
	PageFormatStatement PageFormat = "Statement"
	PageFormatFolio     PageFormat = "Folio"
)

// Orientation is the page orientation: portrait or landscape.
type Orientation string

// Canonical orientations.
const (
	OrientationPortrait  Orientation = "portrait"
	OrientationLandscape Orientation = "landscape"
)

// RenderMetadata is free-form caller metadata forwarded to the API and
// echoed on responses that support it. Values must be primitives (string,
// number, bool); nested maps and slices are rejected at serialise time
// with *Error{Code: ErrCodeInvalidOptions}.
type RenderMetadata = map[string]any

// RenderInput is the marker interface satisfied by [ProjectModeInput] and
// [InlineModeInput] only. External packages cannot satisfy it (the marker
// method is unexported) — that is how Render.PDF / PDFStream / Document
// enforce project-mode-only at compile time.
type RenderInput interface {
	isRenderInput()
}

// Environment is the API environment a stored document belongs to.
type Environment string

// Document environments.
const (
	EnvironmentSandbox Environment = "sandbox"
	EnvironmentLive    Environment = "live"
)

// ProjectModeInput renders against a stored project + template by slug.
// Use [Opt] to set Version with a literal: Version: polipage.Opt("1.0.0").
type ProjectModeInput struct {
	// Required.
	Project  string         `json:"project"`
	Template string         `json:"template"`
	Data     map[string]any `json:"data"`

	// Optional — omitted from the wire when unset.
	Version     *string        `json:"version,omitempty"`
	Format      PageFormat     `json:"format,omitempty"`
	Orientation Orientation    `json:"orientation,omitempty"`
	Locale      string         `json:"locale,omitempty"`
	Metadata    RenderMetadata `json:"metadata,omitempty"`
}

func (ProjectModeInput) isRenderInput() {}

// InlineModeInput renders raw HTML with no project resolution. Use this
// with Render.Preview to validate templates before saving them.
type InlineModeInput struct {
	Data     map[string]any `json:"data"`
	Template string         `json:"template"` // raw HTML

	Format      PageFormat     `json:"format,omitempty"`
	Orientation Orientation    `json:"orientation,omitempty"`
	Locale      string         `json:"locale,omitempty"`
	Metadata    RenderMetadata `json:"metadata,omitempty"`
}

func (InlineModeInput) isRenderInput() {}

// PreviewResult is the result of Render.Preview — paginated HTML, the
// total page count the engine produced, and the environment that served
// the request.
type PreviewResult struct {
	HTML        string      `json:"html"`
	TotalPages  int         `json:"totalPages"`
	Environment Environment `json:"environment"`
}

// RetryEvent is delivered to the OnRetry hook before each retry sleep.
// Aliased to [clientconfig.RetryEvent] so option.WithOnRetry and the
// re-export here can both spell the type identically.
type RetryEvent = clientconfig.RetryEvent

// Opt returns a pointer to v. Use it to set optional pointer-typed fields
// with literal values:
//
//	polipage.ProjectModeInput{Version: polipage.Opt("1.0.0")}
//
// Named for the intent (optional fields) rather than the mechanic
// (pointers) — Go has no inline pointer-to-literal syntax.
func Opt[T any](v T) *T { return &v }
