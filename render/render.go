package render

import (
	"bytes"
	"fmt"

	"github.com/Templetry/engine/ops"
	"github.com/Templetry/engine/source"
)

// Apply executes a Plan against the template FileSet and returns the output
// FileSet. Pure: disk is only touched by WriteDir. Text pass order is
// normative: directives -> identity replacements -> patches.
func Apply(p *ops.Plan, in *source.FileSet) (*source.FileSet, error) {
	out := source.NewFileSet()
	for _, fa := range p.Files {
		f := in.Get(fa.Src)
		if f == nil {
			return nil, fmt.Errorf("plan references missing file %q", fa.Src)
		}
		switch fa.Action {
		case ops.Exclude:
			continue
		case ops.Copy:
			out.Put(fa.Dest, &source.File{
				Data:   append([]byte(nil), f.Data...),
				Exec:   f.Exec,
				Binary: f.Binary,
			})
		case ops.Render:
			data := normalizeEOL(f.Data)
			data, err := FilterDirectives(fa.Src, data, p.Features, p.Variables)
			if err != nil {
				return nil, err
			}
			for _, r := range p.Replacements {
				data = bytes.ReplaceAll(data, []byte(r.From), []byte(r.To))
			}
			for _, patch := range fa.Patches {
				data, err = applyPatch(fa.Src, data, patch, p.Variables)
				if err != nil {
					return nil, fmt.Errorf("%s: patch %s %s: %w", fa.Src, patch.Op, patch.Path, err)
				}
			}
			out.Put(fa.Dest, &source.File{Data: data, Exec: f.Exec})
		default:
			return nil, fmt.Errorf("unknown action %q for %q", fa.Action, fa.Src)
		}
	}
	out.Put(answersPath, &source.File{Data: answersYAML(p)})
	return out, nil
}

// normalizeEOL converts CRLF to LF (spec: output is always LF).
func normalizeEOL(data []byte) []byte {
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}
