package render

import (
	"bytes"
	"encoding/json"

	"github.com/Templetry/engine/manifest"
	jsonpatch "github.com/evanphx/json-patch/v5"
)

// applyPatch applies one RFC 6902 operation to a JSON document. String values
// expand {var.casing} placeholders. Output is re-indented with two spaces
// (deterministic) and ends with a single newline.
func applyPatch(doc []byte, p manifest.Patch, vars map[string]string) ([]byte, error) {
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
	patched, err := patch.Apply(doc)
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
