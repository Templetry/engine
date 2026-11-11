package render

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Templetry/engine/internal/safepath"
	"github.com/Templetry/engine/source"
)

// Contain resolves p beneath root and refuses anything unsafe to write: an
// escape from root, an absolute path, any .git segment (in any spelling the
// platforms fold to it), reserved Windows names, and a path that passes
// through a symlink. The rules live in safepath.
//
// The tarball reader rejects unsafe entries and the planner rejects unsafe
// identity destinations, so a path arriving here has already been checked
// twice. This is the last check before a write, and the only one that sees
// the actual root and the actual disk — which is what makes it worth
// repeating. Every writer (render, update, pieces) goes through it, so the
// CLI and the MCP server are protected by the same code.
func Contain(root, p string) (string, error) {
	return safepath.Resolve(root, p)
}

// WriteDir materializes a FileSet under dir, creating directories as needed.
// The only effectful step of the render pipeline.
//
// Every path is validated before the first byte is written, so a template
// with one bad path leaves the destination untouched instead of half
// written.
func WriteDir(set *source.FileSet, dir string) error {
	fulls := make([]string, 0, set.Len())
	paths := set.Paths()
	for _, p := range paths {
		full, err := Contain(dir, p)
		if err != nil {
			return err
		}
		fulls = append(fulls, full)
	}
	for i, p := range paths {
		f := set.Get(p)
		full := fulls[i]
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if f.Exec {
			mode = 0o755
		}
		if err := os.WriteFile(full, f.Data, mode); err != nil {
			return err
		}
	}
	return nil
}
