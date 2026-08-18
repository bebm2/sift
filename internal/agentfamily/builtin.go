package agentfamily

import (
	"embed"
	"fmt"
	"sort"
)

// builtinFS embeds the family definitions Sift ships with. Each *.yaml file
// holds exactly one family document (family.go's Parse contract).
//
//go:embed builtin/*.yaml
var builtinFS embed.FS

// Builtin decodes and validates every embedded family file. It panics on
// decode/validation failure since the embedded set is a build-time constant,
// not user input; TestBuiltinFamiliesValid guards this at test time so a
// panic can never reach a running daemon.
func Builtin() map[string]*Family {
	out, err := loadFS(builtinFS, "builtin")
	if err != nil {
		panic(fmt.Sprintf("agentfamily: embedded builtin families: %v", err))
	}
	return out
}

// BuiltinIDs returns the built-in family ids in sorted order, for display
// (`sift agent list-families`) and tests.
func BuiltinIDs() []string {
	families := Builtin()
	ids := make([]string, 0, len(families))
	for id := range families {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
