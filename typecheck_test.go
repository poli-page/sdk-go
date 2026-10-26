package polipage

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// jsonFieldNames returns the JSON names of a struct type's exported fields.
func jsonFieldNames(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func TestTypeShape_RenderInputHasOnlyTheSevenDocumentedFields(t *testing.T) {
	t.Parallel()
	got := jsonFieldNames(reflect.TypeFor[RenderInput]())
	want := []string{"data", "format", "locale", "metadata", "orientation", "project", "template"}
	if !slices.Equal(got, want) {
		t.Errorf("RenderInput JSON fields = %v, want %v", got, want)
	}
}

func TestTypeShape_DocumentDescriptorHasNoVersionAndAStringEnvironment(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[DocumentDescriptor]()
	if slices.Contains(jsonFieldNames(typ), "version") {
		t.Error("DocumentDescriptor still has a version field")
	}
	f, ok := typ.FieldByName("Environment")
	if !ok || f.Type != reflect.TypeFor[string]() {
		t.Errorf("DocumentDescriptor.Environment type = %v, want string", f.Type)
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
