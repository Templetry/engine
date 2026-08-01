package render

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Templetry/engine/source"
)

// WriteDir materializes a FileSet under dir, creating directories as needed.
// The only effectful step of the render pipeline.
func WriteDir(set *source.FileSet, dir string) error {
	for _, p := range set.Paths() {
		f := set.Get(p)
		full := filepath.Join(dir, filepath.FromSlash(p))
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
