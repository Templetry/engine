package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/Templetry/engine/manifest"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/goccy/go-yaml"
)

// applyPatch applies one RFC 6902 operation to a structured document. The
// codec is chosen by the target's extension: JSON natively; YAML and TOML
// through a JSON round-trip. String values expand {var.casing} placeholders.
// Patched files are re-serialized deterministically (sorted keys, fixed
// indentation) — surgical data edits, not comment-preserving edits; when a
// format has comments, directives are the mechanism that keeps them.
func applyPatch(file string, doc []byte, p manifest.Patch, vars map[string]string) ([]byte, error) {
	switch strings.ToLower(path.Ext(file)) {
	case ".json":
		return patchJSON(doc, p, vars)
	case ".yml", ".yaml":
		return patchVia(doc, p, vars,
			func(data []byte, v any) error { return yaml.Unmarshal(data, v) },
			marshalYAML)
	case ".toml":
		return patchVia(doc, p, vars,
			func(data []byte, v any) error { return toml.Unmarshal(data, v) },
			marshalTOML)
	default:
		return nil, fmt.Errorf("patches support .json, .yml/.yaml and .toml targets, not %q", path.Ext(file))
	}
}

func patchJSON(doc []byte, p manifest.Patch, vars map[string]string) ([]byte, error) {
	patched, err := runPatch(doc, p, vars)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, patched, "", "  "); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// patchVia converts a YAML/TOML document to JSON, applies the patch there,
// and serializes back with the given marshaller.
func patchVia(doc []byte, p manifest.Patch, vars map[string]string,
	unmarshal func([]byte, any) error, marshal func(any) ([]byte, error)) ([]byte, error) {
	var v any
	if err := unmarshal(doc, &v); err != nil {
		return nil, err
	}
	asJSON, err := json.Marshal(normalizeKeys(v))
	if err != nil {
		return nil, err
	}
	patched, err := runPatch(asJSON, p, vars)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(patched, &out); err != nil {
		return nil, err
	}
	return marshal(out)
}

// runPatch builds and applies the single-op RFC 6902 patch on JSON bytes.
func runPatch(doc []byte, p manifest.Patch, vars map[string]string) ([]byte, error) {
	val := p.Value
	if s, ok := val.(string); ok {
		expanded, err := manifest.Expand(s, vars)
		if err != nil {
			return nil, err
		}
		val = expanded
	}
	op := map[string]any{"op": p.Op, "path": p.Path}
	if p.Op != "remove" {
		op["value"] = val
	}
	raw, err := json.Marshal([]any{op})
	if err != nil {
		return nil, err
	}
	patch, err := jsonpatch.DecodePatch(raw)
	if err != nil {
		return nil, err
	}
	return patch.Apply(doc)
}

// normalizeKeys converts map[any]any trees (some decoders) to map[string]any
// so the value survives a json.Marshal.
func normalizeKeys(v any) any {
	switch t := v.(type) {
	case map[any]any:
		out := map[string]any{}
		for k, val := range t {
			out[fmt.Sprint(k)] = normalizeKeys(val)
		}
		return out
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeKeys(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normalizeKeys(val)
		}
		return t
	default:
		return v
	}
}

// marshalYAML serializes with sorted keys and two-space indent — the same
// document always yields the same bytes (NFR4).
func marshalYAML(v any) ([]byte, error) {
	return yaml.MarshalWithOptions(sortKeys(v), yaml.Indent(2))
}

// marshalTOML serializes deterministically (BurntSushi sorts keys).
func marshalTOML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sortKeys rebuilds maps as yaml.MapSlice in sorted key order so the YAML
// marshaller emits deterministically.
func sortKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := yaml.MapSlice{}
		for _, k := range keys {
			out = append(out, yaml.MapItem{Key: k, Value: sortKeys(t[k])})
		}
		return out
	case []any:
		for i, val := range t {
			t[i] = sortKeys(val)
		}
		return t
	default:
		return v
	}
}
