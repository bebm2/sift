// Command genschema generates JSON Schema (Draft 2020-12) for the seed
// boundary types in package contract via [schemagen]. It is invoked by the
// `//go:generate` directive in contract.go.
//
// Output is deterministic so that the committed files in schema/ are
// diffable. The CI drift check runs `go generate ./...` then
// `git diff --exit-code`: if a struct changes without regenerating its
// schema, CI fails.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/miaoxiaoyong/sift/internal/config"
	"github.com/miaoxiaoyong/sift/internal/contract"
	"github.com/miaoxiaoyong/sift/internal/contract/schemagen"
)

func main() {
	out := flag.String("out", "schema", "output directory for generated schemas")
	flag.Parse()

	types := []any{
		contract.ClosedExample{},
		contract.OpenEnvelopeExample{},
		config.RawConfig{},
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatalf("mkdir %s: %v", *out, err)
	}
	for _, v := range types {
		t, err := schemagen.TargetFor(v)
		if err != nil {
			fatalf("%v", err)
		}
		data, err := schemagen.Generate(t)
		if err != nil {
			fatalf("generate %s: %v", t.Name, err)
		}
		path := filepath.Join(*out, t.Name+".schema.json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			fatalf("write %s: %v", path, err)
		}
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genschema: "+format+"\n", args...)
	os.Exit(1)
}
