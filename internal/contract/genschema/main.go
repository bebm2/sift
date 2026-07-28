// Command genschema generates JSON Schema (Draft 2020-12) for the seed
// boundary types in package contract via reflection. It is invoked by the
// `//go:generate` directive in contract.go.
//
// Output is deterministic (sorted map keys via encoding/json, sorted required
// arrays, stable property order from struct declaration order) so that the
// committed files in schema/ are diffable. The CI drift check runs
// `go generate ./...` then `git diff --exit-code`: if a struct changes without
// regenerating its schema, CI fails.
//
// The generator keeps the struct as the single source of truth (DESIGN §5.2):
// required, type and enum all derive from the Go type, so the schema cannot
// drift from the runtime contract without also changing the struct.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/miaoxiaoyong/sift/internal/contract"
	"github.com/miaoxiaoyong/sift/internal/decode"
)

// target pairs a schema file base name with its type and decode mode.
type target struct {
	name string
	typ  reflect.Type
	mode decode.Mode
}

func main() {
	out := flag.String("out", "schema", "output directory for generated schemas")
	flag.Parse()

	targets := []reflect.Type{
		typeOf(contract.ClosedExample{}),
		typeOf(contract.OpenEnvelopeExample{}),
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatalf("mkdir %s: %v", *out, err)
	}
	for _, typ := range targets {
		mode, err := modeOfType(typ)
		if err != nil {
			fatalf("%s: %v", typ.Name(), err)
		}
		t := target{name: schemaName(typ), typ: typ, mode: mode}
		if err := generate(t, *out); err != nil {
			fatalf("generate %s: %v", t.name, err)
		}
	}
}

// schemaName derives the schema file base name from the type name, converting
// CamelCase to snake_case.
func schemaName(t reflect.Type) string {
	return toSnake(t.Name())
}

// modeOfType reads the decode mode a boundary type declares by embedding
// contract.ClosedType or contract.OpenEnvelopeType. This keeps the struct as
// the single source of truth for its own contract shape: a type cannot be
// registered under the wrong mode in the target list.
func modeOfType(t reflect.Type) (decode.Mode, error) {
	closed := typeOf(contract.ClosedType{})
	open := typeOf(contract.OpenEnvelopeType{})
	mode := decode.Mode(-1)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.Anonymous {
			continue
		}
		switch f.Type {
		case closed:
			if mode == decode.OpenEnvelope {
				return 0, fmt.Errorf("embeds both ClosedType and OpenEnvelopeType")
			}
			mode = decode.Closed
		case open:
			if mode == decode.Closed {
				return 0, fmt.Errorf("embeds both ClosedType and OpenEnvelopeType")
			}
			mode = decode.OpenEnvelope
		}
	}
	if mode == decode.Mode(-1) {
		return 0, fmt.Errorf("must embed contract.ClosedType or contract.OpenEnvelopeType")
	}
	return mode, nil
}

func generate(t target, outDir string) error {
	doc := buildSchema(t)
	data, err := marshalSchema(doc)
	if err != nil {
		return err
	}
	path := filepath.Join(outDir, t.name+".schema.json")
	return os.WriteFile(path, data, 0o644)
}

// buildSchema assembles the top-level schema document for a target.
func buildSchema(t target) map[string]any {
	doc := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     fmt.Sprintf("https://sift.dev/decode/%s.schema.json", t.name),
		"title":   t.typ.Name(),
		"x-sift": map[string]any{
			"decodeMode": modeString(t.mode),
			"sourceType": t.typ.String(),
		},
	}
	obj, req := objectSchema(t.typ)
	doc["type"] = "object"
	doc["properties"] = obj
	if len(req) > 0 {
		doc["required"] = req
	}
	// Closed contracts forbid extra fields; open-envelope contracts allow them
	// for forward compatibility (DESIGN §5.2).
	if t.mode == decode.Closed {
		doc["additionalProperties"] = false
	}
	return doc
}

// objectSchema returns the "properties" object and the "required" array for a
// struct type, recursing into nested structs.
func objectSchema(t reflect.Type) (map[string]any, []string) {
	props := map[string]any{}
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, named := jsonName(f)
		if !named || name == "-" {
			continue
		}
		req := strings.Contains(f.Tag.Get("sift"), "required")
		// Deref the field pointer once so nullability is decided here, not in
		// typeSchema: a required pointer is non-nullable (null is rejected),
		// an optional pointer is nullable. typeSchema keeps its own pointer
		// handling only for slice/map item contexts.
		ft := f.Type
		nullable := false
		if ft.Kind() == reflect.Pointer {
			nullable = !req
			ft = ft.Elem()
		}
		props[name] = typeSchema(ft, nullable)
		if req {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	return props, required
}

// typeSchema emits the schema fragment for a Go type.
func typeSchema(t reflect.Type, nullable bool) map[string]any {
	if t.Kind() == reflect.Pointer {
		// An inner pointer (e.g. *T inside a struct) is nullable regardless of
		// the caller's flag.
		return typeSchema(t.Elem(), true)
	}
	s := map[string]any{}
	switch t.Kind() {
	case reflect.String:
		s["type"] = "string"
		if allowed := enumValues(t); allowed != nil {
			s["enum"] = allowed
		}
	case reflect.Bool:
		s["type"] = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s["type"] = "integer"
	case reflect.Float32, reflect.Float64:
		s["type"] = "number"
	case reflect.Struct:
		if t.Name() == "" {
			s["type"] = "object"
			break
		}
		props, req := objectSchema(t)
		s["type"] = "object"
		s["properties"] = props
		if len(req) > 0 {
			s["required"] = req
		}
		// Nested owned contracts default to closed; an open-envelope inner
		// object would opt in explicitly when such a type exists.
		s["additionalProperties"] = false
	case reflect.Slice, reflect.Array:
		s["type"] = "array"
		s["items"] = typeSchema(t.Elem(), false)
	case reflect.Map:
		s["type"] = "object"
		s["additionalProperties"] = typeSchema(t.Elem(), false)
	default:
		// Unsupported kinds surface as a clearly-marked fragment so a diff
		// flags the gap rather than emitting a silently-wrong schema.
		s["description"] = "unsupported Go kind: " + t.Kind().String()
	}
	if nullable {
		s = withNullable(s)
	}
	return s
}

// withNullable widens a "type" keyword to admit null. Nullable enums are not
// exercised by the seed types; when one is needed it must extend the generator
// rather than emit a schema that silently rejects null.
func withNullable(s map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range s {
		out[k] = v
	}
	switch tv := out["type"].(type) {
	case string:
		out["type"] = []string{tv, "null"}
	case []string:
		already := false
		for _, t := range tv {
			if t == "null" {
				already = true
				break
			}
		}
		if !already {
			out["type"] = append(tv, "null")
		}
	}
	return out
}

// enumValues returns the allowed values for a named string type implementing
// decode.Enumerated, or nil if the type is not an enum.
func enumValues(t reflect.Type) []string {
	z := reflect.Zero(t).Interface()
	e, ok := z.(decode.Enumerated)
	if !ok {
		return nil
	}
	out := make([]string, len(e.EnumValues()))
	copy(out, e.EnumValues())
	return out
}

func jsonName(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name, true
	}
	name := strings.Split(tag, ",")[0]
	if name == "-" {
		return "-", false
	}
	if name == "" {
		return f.Name, true
	}
	return name, true
}

func modeString(m decode.Mode) string {
	switch m {
	case decode.Closed:
		return "closed"
	case decode.OpenEnvelope:
		return "open-envelope"
	default:
		return fmt.Sprintf("mode(%d)", int(m))
	}
}

func typeOf(v any) reflect.Type {
	return reflect.TypeOf(v)
}

// toSnake converts CamelCase to snake_case. It keeps runs of uppercase
// acronyms together except for the final capital of a run followed by a
// lowercase letter (e.g. "OpenEnvelopeExample" -> "openenvelope_example",
// "HTTPRequest" -> "http_request").
func toSnake(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		isUpper := c >= 'A' && c <= 'Z'
		if isUpper && i > 0 {
			prevLower := s[i-1] >= 'a' && s[i-1] <= 'z'
			nextLower := i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z'
			if prevLower || nextLower {
				b = append(b, '_')
			}
		}
		if isUpper {
			b = append(b, c+('a'-'A'))
		} else {
			b = append(b, c)
		}
	}
	return string(b)
}

// marshalSchema renders deterministically with sorted keys and two-space
// indentation, followed by a trailing newline.
func marshalSchema(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genschema: "+format+"\n", args...)
	os.Exit(1)
}
