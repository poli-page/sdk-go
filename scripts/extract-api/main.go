// Command extract-api walks the public Poli Page Go SDK surface with
// go/packages + go/doc and emits the docs/src/content/docs/reference/
// tree consumed by the Starlight site.
//
// Output shape is defined by docs/src/preset/ (the §4 reference contract):
//
//	reference/
//	  client.mdx
//	  methods/<slug>.mdx       (one per public method, see methodTargets)
//	  types.mdx
//	  errors.mdx
//	  runtime-support.mdx
//	  _meta.json
//
// Each method page includes a verbatim copy of examples/<slug>/main.go —
// the example is what's tested by the SDK's CI, so the docs cannot drift
// from working code.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	moduleName       = "github.com/poli-page/sdk-go"
	extractorVersion = "0.1.0"
)

func main() {
	here, err := os.Getwd()
	must(err)

	// The extractor is invoked from docs/ via `npm run extract`. Climb to repo root.
	repoRoot := here
	if filepath.Base(here) == "docs" {
		repoRoot = filepath.Dir(here)
	}
	// When invoked as `go run ./scripts/extract-api/main.go`, here is repo root.
	// When invoked via `npm run extract` from docs/, here is docs/. Both work.

	refOut := filepath.Join(repoRoot, "docs", "src", "content", "docs", "reference")
	methodsOut := filepath.Join(refOut, "methods")
	examplesRoot := filepath.Join(repoRoot, "examples")

	// 1. Clear previous output and recreate.
	if err := os.RemoveAll(refOut); err != nil && !os.IsNotExist(err) {
		log.Fatalf("extract-api: cannot clear %s: %v", refOut, err)
	}
	must(os.MkdirAll(methodsOut, 0o755))

	// 2. Parse the SDK packages with go/parser and feed go/doc.
	pkgDocs := loadDocs(repoRoot)

	// 3. Build each page.
	must(os.WriteFile(filepath.Join(refOut, "client.mdx"), []byte(renderClientPage(pkgDocs)), 0o644))

	for _, m := range methodTargets {
		examplePath := filepath.Join(examplesRoot, m.exampleDir, "main.go")
		example, err := os.ReadFile(examplePath)
		if err != nil {
			log.Fatalf("extract-api: example file missing for %s: %s", m.slug, examplePath)
		}
		page := renderMethodPage(m, pkgDocs, string(example))
		must(os.WriteFile(filepath.Join(methodsOut, m.slug+".mdx"), []byte(page), 0o644))
	}

	must(os.WriteFile(filepath.Join(refOut, "types.mdx"), []byte(renderTypesPage(pkgDocs)), 0o644))
	must(os.WriteFile(filepath.Join(refOut, "errors.mdx"), []byte(renderErrorsPage()), 0o644))
	must(os.WriteFile(filepath.Join(refOut, "runtime-support.mdx"), []byte(renderRuntimeSupportPage()), 0o644))

	meta := buildMetaSidecar()
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(refOut, "_meta.json"), append(metaBytes, '\n'), 0o644))

	fmt.Printf("extract-api: wrote %s\n", refOut)
}

func must(err error) {
	if err != nil {
		log.Fatalf("extract-api: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Package loading via go/parser + go/doc
// ---------------------------------------------------------------------------

type pkgInfo struct {
	docPkg *doc.Package
}

// loadDocs parses the two packages the SDK exposes (root + option) and
// returns their go/doc views. Tests / internal packages are skipped.
func loadDocs(repoRoot string) map[string]*pkgInfo {
	result := map[string]*pkgInfo{
		"":       parsePkg(repoRoot, "polipage"),
		"option": parsePkg(filepath.Join(repoRoot, "option"), "option"),
	}
	return result
}

func parsePkg(dir, name string) *pkgInfo {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("extract-api: read dir %s: %v", dir, err)
	}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		path := filepath.Join(dir, n)
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			log.Fatalf("extract-api: parse %s: %v", path, err)
		}
		if f.Name.Name != name {
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		log.Fatalf("extract-api: package %s not found in %s", name, dir)
	}
	docPkg, err := doc.NewFromFiles(fset, files, "./", doc.AllDecls)
	if err != nil {
		log.Fatalf("extract-api: doc.NewFromFiles %s: %v", dir, err)
	}
	return &pkgInfo{docPkg: docPkg}
}

// ---------------------------------------------------------------------------
// Helpers shared by every page renderer
// ---------------------------------------------------------------------------

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, ". "); i >= 0 {
		return strings.TrimSpace(s[:i+1])
	}
	if i := strings.Index(s, ".\n"); i >= 0 {
		return strings.TrimSpace(s[:i+1])
	}
	return s
}

// cleanDoc returns the doc string ready for inclusion in an MDX lede:
//
//   - Whitespace is trimmed.
//   - Godoc-style identifier links like `[Render.PDF]` are unwrapped to
//     `Render.PDF`. MDX would otherwise parse them as Markdown links and
//     emit broken hrefs.
//   - Curly braces `{` `}` (e.g. in `*Error{Code: ErrCodeFoo}`) are
//     backslash-escaped — bare braces start an MDX expression and crash
//     the build.
//   - Bare angle brackets that look like JSX tags are escaped.
func cleanDoc(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = godocLinkRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(match, "["), "]")
		if strings.HasPrefix(inner, "http") || strings.Contains(inner, ":") {
			return match
		}
		return inner
	})
	s = strings.ReplaceAll(s, "{", "\\{")
	s = strings.ReplaceAll(s, "}", "\\}")
	s = lessThanIdentRe.ReplaceAllString(s, "&lt;$1")
	return s
}

var (
	godocLinkRe     = regexp.MustCompile(`\[[A-Za-z_*][A-Za-z0-9_.*]*\]`)
	lessThanIdentRe = regexp.MustCompile(`<([A-Za-z/*])`)
)

func escapeFrontmatter(s string) string {
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 150 {
		s = s[:150]
	}
	return s
}

// findFunc returns the *doc.Func with the given name on the receiver type
// (empty receiver matches top-level functions).
func findFunc(pkg *doc.Package, recv, name string) *doc.Func {
	if recv == "" {
		for _, f := range pkg.Funcs {
			if f.Name == name {
				return f
			}
		}
		return nil
	}
	for _, t := range pkg.Types {
		if t.Name != recv {
			continue
		}
		for _, m := range t.Methods {
			if m.Name == name {
				return m
			}
		}
	}
	return nil
}

func findType(pkg *doc.Package, name string) *doc.Type {
	for _, t := range pkg.Types {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// renderFuncSignature returns the canonical Go signature for a function or
// method, e.g.:
//
//	func (r *Render) PDF(ctx context.Context, in ProjectModeInput, opts ...option.RequestOption) ([]byte, error)
func renderFuncSignature(fn *doc.Func) string {
	if fn == nil || fn.Decl == nil {
		return ""
	}
	// We have the *ast.FuncDecl; format manually to control spacing.
	d := fn.Decl
	var sb strings.Builder
	sb.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		sb.WriteString("(")
		sb.WriteString(formatField(d.Recv.List[0]))
		sb.WriteString(") ")
	}
	sb.WriteString(d.Name.Name)
	sb.WriteString("(")
	if d.Type.Params != nil {
		sb.WriteString(formatFieldList(d.Type.Params))
	}
	sb.WriteString(")")
	if d.Type.Results != nil {
		sb.WriteString(" ")
		results := formatFieldList(d.Type.Results)
		if len(d.Type.Results.List) == 1 && len(d.Type.Results.List[0].Names) == 0 {
			sb.WriteString(results)
		} else {
			sb.WriteString("(")
			sb.WriteString(results)
			sb.WriteString(")")
		}
	}
	return sb.String()
}

func formatFieldList(fl *ast.FieldList) string {
	parts := make([]string, 0, len(fl.List))
	for _, f := range fl.List {
		parts = append(parts, formatField(f))
	}
	return strings.Join(parts, ", ")
}

func formatField(f *ast.Field) string {
	typeStr := formatExpr(f.Type)
	if len(f.Names) == 0 {
		return typeStr
	}
	names := make([]string, 0, len(f.Names))
	for _, n := range f.Names {
		names = append(names, n.Name)
	}
	return strings.Join(names, ", ") + " " + typeStr
}

func formatExpr(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + formatExpr(t.X)
	case *ast.SelectorExpr:
		return formatExpr(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + formatExpr(t.Elt)
	case *ast.MapType:
		return "map[" + formatExpr(t.Key) + "]" + formatExpr(t.Value)
	case *ast.Ellipsis:
		return "..." + formatExpr(t.Elt)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		var sb strings.Builder
		sb.WriteString("func(")
		if t.Params != nil {
			sb.WriteString(formatFieldList(t.Params))
		}
		sb.WriteString(")")
		if t.Results != nil && len(t.Results.List) > 0 {
			sb.WriteString(" ")
			if len(t.Results.List) == 1 && len(t.Results.List[0].Names) == 0 {
				sb.WriteString(formatExpr(t.Results.List[0].Type))
			} else {
				sb.WriteString("(")
				sb.WriteString(formatFieldList(t.Results))
				sb.WriteString(")")
			}
		}
		return sb.String()
	case *ast.IndexExpr:
		return formatExpr(t.X) + "[" + formatExpr(t.Index) + "]"
	case *ast.ChanType:
		return "chan " + formatExpr(t.Value)
	default:
		return fmt.Sprintf("%T", e)
	}
}

// ---------------------------------------------------------------------------
// Method targets — the public surface
// ---------------------------------------------------------------------------

type methodTarget struct {
	slug        string // file slug under reference/methods/
	displayName string // "Render.PDF", "RenderToFile"
	receiver    string // "Render", "Documents", "DocumentDescriptor", "" for top-level fn
	method      string // method or function name
	exampleDir  string // examples/<dir>/main.go
	errorCodes  []errorRow
}

type errorRow struct {
	code string
	when string
}

func defaultErrors(extra ...errorRow) []errorRow {
	base := []errorRow{
		{"VALIDATION_ERROR", "Request body failed validation."},
		{"NOT_FOUND", "Project/template slug or document does not exist."},
		{"QUOTA_EXCEEDED", "Rate limit or monthly quota reached. Retryable."},
		{"timeout", "Per-request deadline exceeded. Retryable."},
		{"network_error", "TCP/TLS failure reaching the API. Retryable."},
		{"INTERNAL_ERROR", "API returned 5xx. Retryable."},
	}
	return append(base, extra...)
}

func documentErrors() []errorRow {
	return []errorRow{
		{"DOCUMENT_NOT_FOUND", "No stored document matches the supplied id."},
		{"INVALID_API_KEY", "The API key is malformed or revoked."},
		{"INTERNAL_ERROR", "API returned 5xx. Retryable."},
	}
}

var methodTargets = []methodTarget{
	{
		slug: "render-pdf", displayName: "Render.PDF",
		receiver: "Render", method: "PDF",
		exampleDir: "render-pdf",
		errorCodes: defaultErrors(errorRow{"DOWNLOAD_FAILED", "Presigned URL fetch failed."}),
	},
	{
		slug: "render-pdf-stream", displayName: "Render.PDFStream",
		receiver: "Render", method: "PDFStream",
		exampleDir: "render-pdf-stream",
		errorCodes: defaultErrors(errorRow{"DOWNLOAD_FAILED", "Presigned URL fetch failed."}),
	},
	{
		slug: "render-preview", displayName: "Render.Preview",
		receiver: "Render", method: "Preview",
		exampleDir: "render-preview",
		errorCodes: defaultErrors(),
	},
	{
		slug: "render-document", displayName: "Render.Document",
		receiver: "Render", method: "Document",
		exampleDir: "render-document",
		errorCodes: defaultErrors(errorRow{"PROJECT_REQUIRED_FOR_DOCUMENT", "Render.Document requires ProjectModeInput.Project."}),
	},
	{
		slug: "documents-get", displayName: "Documents.Get",
		receiver: "Documents", method: "Get",
		exampleDir: "documents-get",
		errorCodes: documentErrors(),
	},
	{
		slug: "documents-preview", displayName: "Documents.Preview",
		receiver: "Documents", method: "Preview",
		exampleDir: "documents-preview",
		errorCodes: documentErrors(),
	},
	{
		slug: "documents-thumbnails", displayName: "Documents.Thumbnails",
		receiver: "Documents", method: "Thumbnails",
		exampleDir: "documents-thumbnails",
		errorCodes: append(documentErrors(), errorRow{"VALIDATION_ERROR", "Invalid ThumbnailOptions (width required, JPEG quality range)."}),
	},
	{
		slug: "documents-delete", displayName: "Documents.Delete",
		receiver: "Documents", method: "Delete",
		exampleDir: "documents-delete",
		errorCodes: append(documentErrors(), errorRow{"GONE", "Document was already deleted."}),
	},
	{
		slug: "render-to-file", displayName: "RenderToFile",
		receiver: "", method: "RenderToFile",
		exampleDir: "render-to-file",
		errorCodes: append(defaultErrors(errorRow{"DOWNLOAD_FAILED", "Presigned URL fetch failed."}),
			errorRow{"io_failed", "Local filesystem write failed."}),
	},
}

// ---------------------------------------------------------------------------
// Page renderers
// ---------------------------------------------------------------------------

func renderClientPage(pkgs map[string]*pkgInfo) string {
	polipage := pkgs[""].docPkg
	var lede string
	if t := findType(polipage, "Client"); t != nil {
		lede = cleanDoc(t.Doc)
	}
	if lede == "" {
		lede = "Client is the Poli Page SDK entry point. Construct one via NewClient and reuse it for the lifetime of the process."
	}
	// Signature of NewClient.
	sig := "func NewClient(opts ...option.RequestOption) *Client"
	if fn := findFunc(polipage, "", "NewClient"); fn != nil {
		s := renderFuncSignature(fn)
		if s != "" {
			sig = s
		}
	}

	return `---
title: Client
description: The polipage.Client type — the only entry point to the Go SDK.
---

import MethodSignature from '@preset/components/MethodSignature.astro';

<MethodSignature lang="go" code={` + "`" + sig + "`" + `} />

` + lede + `

## Constructor

` + "`NewClient`" + ` accepts a variadic list of ` + "`option.RequestOption`" + ` values. The only required option is ` + "`option.WithAPIKey`" + `; everything else has a sensible default. See [Types](../types/) for the full ` + "`option`" + ` surface.

## Namespaces

The client exposes two namespaces:

- [` + "`Render`" + `](./methods/render-pdf/) — render PDFs (in-memory bytes, streaming, or stored documents).
- [` + "`Documents`" + `](./methods/documents-get/) — fetch, preview, thumbnail, or delete stored documents.

The top-level helper [` + "`RenderToFile`" + `](./methods/render-to-file/) is a thin convenience over ` + "`Render.PDFStream`" + ` that writes directly to disk.

## See also
- [Types](../types/)
- [Errors](../errors/)
- [Runtime support](../runtime-support/)
`
}

func renderMethodPage(m methodTarget, pkgs map[string]*pkgInfo, example string) string {
	polipage := pkgs[""].docPkg
	fn := findFunc(polipage, m.receiver, m.method)
	var (
		signature string
		summary   string
	)
	if fn != nil {
		signature = renderFuncSignature(fn)
		summary = cleanDoc(fn.Doc)
	}
	if signature == "" {
		signature = m.displayName + "(...)"
	}
	if summary == "" {
		summary = m.displayName + " method."
	}
	lede := summary
	description := escapeFrontmatter(firstSentence(summary))
	if description == "" {
		description = m.displayName + " method."
	}

	paramsBlock := renderParamsBlock(fn)
	returnsBlock := renderReturnsBlock(fn)
	errorsBlock := renderErrorsBlock(m.errorCodes)

	return `---
title: ` + m.displayName + `
description: ` + description + `
sidebar:
  label: ` + m.displayName + `
---

import MethodSignature from '@preset/components/MethodSignature.astro';
import ParamsTable from '@preset/components/ParamsTable.astro';
import ErrorTable from '@preset/components/ErrorTable.astro';

<MethodSignature lang="go" code={` + "`" + signature + "`" + `} />

` + lede + `
` + paramsBlock + returnsBlock + errorsBlock + `
## Example

` + "```go\n" + strings.TrimRight(example, "\n") + "\n```" + `

## See also
- [Errors](../../errors/)
- [Configuration](../../../concepts/configuration/)
`
}

type paramRow struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

func renderParamsBlock(fn *doc.Func) string {
	if fn == nil || fn.Decl == nil || fn.Decl.Type.Params == nil {
		return ""
	}
	rows := []paramRow{}
	for _, f := range fn.Decl.Type.Params.List {
		typeStr := formatExpr(f.Type)
		_, isVariadic := f.Type.(*ast.Ellipsis)
		required := !isVariadic
		for _, n := range f.Names {
			rows = append(rows, paramRow{
				Name:        n.Name,
				Type:        typeStr,
				Required:    required,
				Description: "(no description)",
			})
		}
		if len(f.Names) == 0 {
			rows = append(rows, paramRow{
				Name: "_", Type: typeStr, Required: required, Description: "(no description)",
			})
		}
	}
	if len(rows) == 0 {
		return ""
	}
	b, _ := json.Marshal(rows)
	return "\n## Parameters\n\n<ParamsTable params={" + string(b) + "} />\n"
}

func renderReturnsBlock(fn *doc.Func) string {
	if fn == nil || fn.Decl == nil || fn.Decl.Type.Results == nil {
		return ""
	}
	parts := []string{}
	for _, f := range fn.Decl.Type.Results.List {
		parts = append(parts, formatExpr(f.Type))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n## Returns\n\n`" + strings.Join(parts, ", ") + "`\n"
}

func renderErrorsBlock(rows []errorRow) string {
	if len(rows) == 0 {
		return ""
	}
	type out struct {
		Code string `json:"code"`
		When string `json:"when"`
	}
	jsonRows := make([]out, 0, len(rows))
	for _, r := range rows {
		jsonRows = append(jsonRows, out{Code: r.code, When: r.when})
	}
	b, _ := json.Marshal(jsonRows)
	return "\n## Errors\n\n<ErrorTable errors={" + string(b) + "} />\n"
}

// ---------------------------------------------------------------------------
// Types page
// ---------------------------------------------------------------------------

var publicTypes = []string{
	"Client",
	"Render",
	"Documents",
	"RenderInput",
	"ProjectModeInput",
	"InlineModeInput",
	"PreviewResult",
	"DocumentPreviewResult",
	"DocumentDescriptor",
	"Thumbnail",
	"ThumbnailOptions",
	"RenderMetadata",
	"PageFormat",
	"Orientation",
	"Environment",
	"ThumbnailFormat",
	"RetryEvent",
	"Error",
}

func renderTypesPage(pkgs map[string]*pkgInfo) string {
	polipage := pkgs[""].docPkg

	var sb strings.Builder
	sb.WriteString(`---
title: Types
description: Public types exported from github.com/poli-page/sdk-go and its option subpackage.
---

The Go SDK exposes the types below. Import any of them from the top-level ` + "`polipage`" + ` package (or the ` + "`option`" + ` subpackage for the functional options).

`)
	sb.WriteString("```go\n")
	sb.WriteString("import (\n")
	sb.WriteString("    polipage \"github.com/poli-page/sdk-go\"\n")
	sb.WriteString("    \"github.com/poli-page/sdk-go/option\"\n")
	sb.WriteString(")\n")
	sb.WriteString("```\n\n")

	for _, name := range publicTypes {
		t := findType(polipage, name)
		if t == nil {
			continue
		}
		sb.WriteString("### `" + name + "`\n\n")
		doc := cleanDoc(t.Doc)
		if doc == "" {
			doc = "_(See the source for the full definition.)_"
		}
		sb.WriteString(doc + "\n\n")
	}

	// Functional options from the option package.
	sb.WriteString("## Functional options\n\n")
	sb.WriteString("Options live in the `option` subpackage and produce `option.RequestOption` values.\n\n")
	optPkg := pkgs["option"].docPkg
	for _, f := range optPkg.Funcs {
		if !ast.IsExported(f.Name) {
			continue
		}
		sb.WriteString("- `option." + f.Name + "` — " + firstSentence(cleanDoc(f.Doc)) + "\n")
	}
	sb.WriteString("\nSee [the source on GitHub](https://github.com/poli-page/sdk-go/blob/main/types.go) for the full struct definitions.\n")
	return sb.String()
}

// ---------------------------------------------------------------------------
// Errors page
// ---------------------------------------------------------------------------

func renderErrorsPage() string {
	type row struct {
		Code string `json:"code"`
		When string `json:"when"`
	}
	sdkInternal := []row{
		{"invalid_options", "Constructor options are missing or malformed."},
		{"network_error", "TCP/TLS-level failure reaching the API. Retryable."},
		{"timeout", "The request did not complete within the configured WithTimeout. Retryable."},
		{"aborted", "Caller context was cancelled. Not retryable."},
		{"io_failed", "Local filesystem write failed in RenderToFile."},
		{"DOWNLOAD_FAILED", "Fetching a presigned PDF URL failed."},
	}
	apiAuth := []row{
		{"MISSING_API_KEY", "No API key in the request."},
		{"INVALID_API_KEY", "The API key is malformed or revoked."},
	}
	apiBilling := []row{
		{"PAYMENT_REQUIRED", "Organization billing is past due."},
		{"FORBIDDEN", "The key does not have access to the requested resource."},
		{"ORGANIZATION_CANCELLED", "The organization has been cancelled."},
		{"ORGANIZATION_PURGED", "The organization has been purged."},
	}
	apiNotFound := []row{
		{"NOT_FOUND", "The project/template slug does not exist or is not published."},
		{"VERSION_NOT_FOUND", "The pinned version does not exist for this template."},
		{"DOCUMENT_NOT_FOUND", "No stored document matches the supplied id."},
		{"GONE", "The resource existed but has been deleted."},
	}
	apiValidation := []row{
		{"VALIDATION_ERROR", "Data does not satisfy the template schema."},
		{"MISSING_DATA", "Request body lacks the required Data field."},
		{"MISSING_PROJECT_OR_TEMPLATE", "Project mode call without both Project and Template."},
		{"MISSING_TEMPLATE_SLUG", "Template slug is missing."},
		{"PROJECT_REQUIRED_FOR_DOCUMENT", "Render.Document requires ProjectModeInput.Project."},
		{"INVALID_VERSION_FORMAT", "The Version string is not a valid semver."},
		{"VERSION_REQUIRED", "Live keys require a pinned Version."},
		{"INVALID_VERSION_FOR_KEY_ENV", "Sandbox key targeting a live-only version, or vice versa."},
	}
	apiRate := []row{
		{"QUOTA_EXCEEDED", "Per-key rate limit or monthly quota reached. Retryable."},
		{"OVERAGE_CAP_EXCEEDED", "Hard overage cap reached. Not retryable."},
	}
	apiServer := []row{
		{"INTERNAL_ERROR", "The API returned 5xx. Retryable."},
	}

	j := func(rows []row) string {
		b, _ := json.Marshal(rows)
		return string(b)
	}

	return `---
title: Errors
description: All error codes returned by the SDK, grouped by source.
---

import ErrorTable from '@preset/components/ErrorTable.astro';

Every failure returned by the SDK is a ` + "`*polipage.Error`" + ` with a ` + "`Code`" + `. SDK-internal codes are lowercase; codes from the API are uppercase.

## SDK-internal

<ErrorTable errors={` + j(sdkInternal) + `} />

## Authentication

<ErrorTable errors={` + j(apiAuth) + `} />

## Billing and lifecycle

<ErrorTable errors={` + j(apiBilling) + `} />

## Not found

<ErrorTable errors={` + j(apiNotFound) + `} />

## Validation

<ErrorTable errors={` + j(apiValidation) + `} />

## Rate and quota

<ErrorTable errors={` + j(apiRate) + `} />

## Server

<ErrorTable errors={` + j(apiServer) + `} />
`
}

// ---------------------------------------------------------------------------
// Runtime support
// ---------------------------------------------------------------------------

func renderRuntimeSupportPage() string {
	return `---
title: Runtime support
description: Supported Go versions and operating systems for github.com/poli-page/sdk-go.
---

import RuntimeMatrix from '@preset/components/RuntimeMatrix.astro';

The Go SDK is built and tested against the matrix below.

<RuntimeMatrix matrix={{
  runtimes: ['1.25', '1.26'],
  os: ['linux', 'macos', 'windows'],
  cells: {
    '1.25': { linux: 'tested', macos: 'supported', windows: 'supported' },
    '1.26': { linux: 'tested', macos: 'tested',    windows: 'tested' },
  },
}} />

The minimum supported Go version is **1.25**. The SDK tracks Go's own support window (the latest two minors); older versions may work but are not tested.

## Dependencies

The SDK has no third-party dependencies. It uses only the Go standard library — ` + "`net/http`" + `, ` + "`encoding/json`" + `, ` + "`log/slog`" + `, ` + "`context`" + `, and ` + "`crypto/rand`" + ` for the UUID v4 idempotency key.
`
}

// ---------------------------------------------------------------------------
// _meta.json
// ---------------------------------------------------------------------------

func buildMetaSidecar() any {
	type method struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	methods := make([]method, 0, len(methodTargets))
	for _, m := range methodTargets {
		methods = append(methods, method{Slug: m.slug, Name: m.displayName})
	}
	type errCode struct {
		Code string `json:"code"`
	}
	codes := []string{
		"invalid_options", "network_error", "timeout", "aborted",
		"io_failed", "DOWNLOAD_FAILED",
		"MISSING_API_KEY", "INVALID_API_KEY",
		"PAYMENT_REQUIRED", "FORBIDDEN", "ORGANIZATION_CANCELLED", "ORGANIZATION_PURGED",
		"NOT_FOUND", "VERSION_NOT_FOUND", "DOCUMENT_NOT_FOUND", "GONE",
		"VALIDATION_ERROR", "MISSING_DATA", "MISSING_PROJECT_OR_TEMPLATE",
		"MISSING_TEMPLATE_SLUG", "PROJECT_REQUIRED_FOR_DOCUMENT",
		"INVALID_VERSION_FORMAT", "VERSION_REQUIRED", "INVALID_VERSION_FOR_KEY_ENV",
		"QUOTA_EXCEEDED", "OVERAGE_CAP_EXCEEDED",
		"INTERNAL_ERROR",
	}
	errs := make([]errCode, 0, len(codes))
	for _, c := range codes {
		errs = append(errs, errCode{Code: c})
	}
	return map[string]any{
		"language": "go",
		"package": map[string]any{
			"kind": "go-module",
			"path": moduleName,
		},
		"extractedAt":      time.Now().UTC().Format(time.RFC3339),
		"extractorVersion": extractorVersion,
		"client":           map[string]any{"name": "Client", "kind": "struct"},
		"methods":          methods,
		"errors":           errs,
	}
}
