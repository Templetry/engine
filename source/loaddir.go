package source

import (
	"io/fs"
	"os"
	"path/filepath"
)

// LoadDir reads a template directory into a FileSet. The .git directory is
// skipped; everything else is loaded, with binary detection applied.
func LoadDir(dir string) (*FileSet, error) {
	set := NewFileSet()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		set.Put(filepath.ToSlash(rel), &File{
			Data:   data,
			Exec:   info.Mode()&0o111 != 0,
			Binary: IsBinary(data),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return set, nil
}
