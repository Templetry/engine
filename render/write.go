package render

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Templetry/engine/source"
)

// Contain resolves p beneath root and refuses anything that escapes it.
//
// The tarball reader rejects unsafe entries and the planner rejects unsafe
// identity destinations, so a path arriving here has already been checked
// twice. This is the last check before a write, and the only one that sees
// the actual root — which is what makes it worth repeating: being wrong here
// means a file outside the directory the user named.
func Contain(root, p string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(abs, filepath.FromSlash(p))
	rel, err := filepath.Rel(abs, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to write outside %s: %q", root, p)
	}
	return full, nil
}

// WriteDir materializes a FileSet under dir, creating directories as needed.
// The only effectful step of the render pipeline.
func WriteDir(set *source.FileSet, dir string) error {
	for _, p := range set.Paths() {
		f := set.Get(p)
		full, err := Contain(dir, p)
		if err != nil {
			return err
		}
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
