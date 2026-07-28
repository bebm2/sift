package config

// This file is the sole YAML entry point (config.md §1.2, §4 step 2). The
// decode gateway speaks JSON only; on-disk config.yaml is converted to JSON
// here under the four strict rules, then handed to [decode.Closed]:
//
//   - exactly one document (multi-document input is rejected),
//   - no duplicate mapping keys,
//   - no non-string mapping keys,
//   - no alias cycles.
//
// It depends on gopkg.in/yaml.v3, a pure-Go parser, so CGO_ENABLED=0 holds.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// Sentinels surfaced by YAMLToJSON so the loader can distinguish an empty
// present file (which the spec treats as distinct from an absent file).
var (
	ErrEmptyConfigFile = errors.New("config: file exists but is empty; delete it to use zero-config defaults")
	ErrMultiDocument   = errors.New("config: YAML multi-document input is not allowed")
)

// YAMLToJSON converts a single strict YAML document to compact JSON suitable
// for [decode.Closed]. See the file comment for the enforced rules.
func YAMLToJSON(data []byte) ([]byte, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, ErrEmptyConfigFile
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))

	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("config: parse YAML: %w", err)
	}
	// A second decodable value means the input carried more than one document.
	var probe yaml.Node
	if err := dec.Decode(&probe); err == nil || !errors.Is(err, io.EOF) {
		return nil, ErrMultiDocument
	}

	val, err := nodeToValue(&doc, map[*yaml.Node]bool{})
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("config: encode YAML as JSON: %w", err)
	}
	return out, nil
}

// nodeToValue walks a yaml.Node tree into a JSON-compatible Go value while
// enforcing duplicate-key, string-key and alias-cycle rules. path tracks the
// container nodes currently on the recursion stack so an alias pointing back
// into them is detected as a cycle.
func nodeToValue(n *yaml.Node, path map[*yaml.Node]bool) (any, error) {
	if n == nil {
		return nil, nil
	}
	if n.Kind == yaml.AliasNode {
		t := n.Alias
		if t == nil {
			return nil, errors.New("config: unresolvable YAML alias")
		}
		if path[t] {
			return nil, errors.New("config: YAML alias cycle detected")
		}
		return nodeToValue(t, path)
	}

	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return map[string]any{}, nil
		}
		return nodeToValue(n.Content[0], path)
	case yaml.ScalarNode:
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, fmt.Errorf("config: decode YAML scalar: %w", err)
		}
		return v, nil
	case yaml.SequenceNode:
		path[n] = true
		defer delete(path, n)
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			cv, err := nodeToValue(c, path)
			if err != nil {
				return nil, err
			}
			out = append(out, cv)
		}
		return out, nil
	case yaml.MappingNode:
		path[n] = true
		defer delete(path, n)
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, err := mapKey(n.Content[i], path)
			if err != nil {
				return nil, err
			}
			if _, dup := out[key]; dup {
				return nil, fmt.Errorf("config: duplicate YAML map key %q", key)
			}
			vv, err := nodeToValue(n.Content[i+1], path)
			if err != nil {
				return nil, err
			}
			out[key] = vv
		}
		return out, nil
	default:
		return nil, fmt.Errorf("config: unsupported YAML node kind %d", n.Kind)
	}
}

// mapKey resolves a mapping key node to a string, rejecting aliases, non-string
// tags and the merge key "<<". YAML map keys must be strings (config.md §4.2).
func mapKey(kn *yaml.Node, path map[*yaml.Node]bool) (string, error) {
	n := kn
	if n.Kind == yaml.AliasNode {
		t := n.Alias
		if t == nil {
			return "", errors.New("config: unresolvable YAML alias in map key")
		}
		if path[t] {
			return "", errors.New("config: YAML alias cycle detected")
		}
		n = t
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return "", fmt.Errorf("config: non-string YAML map key (resolved tag %q)", n.Tag)
	}
	if n.Value == "<<" {
		return "", errors.New("config: YAML merge key << is not supported")
	}
	return n.Value, nil
}
