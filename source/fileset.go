package source

import (
	"bytes"
	"sort"
)

// File is one template file held in memory.
type File struct {
	Data   []byte
	Exec   bool // execute permission (preserved from source when available)
	Binary bool // never scanned for directives, copied byte-for-byte
}

// FileSet is an in-memory file tree keyed by slash-separated relative paths.
// Iteration is always in sorted path order (determinism, NFR4).
type FileSet struct {
	files map[string]*File
}

func NewFileSet() *FileSet {
	return &FileSet{files: map[string]*File{}}
}

func (s *FileSet) Put(path string, f *File) {
	s.files[path] = f
}

func (s *FileSet) Get(path string) *File {
	return s.files[path]
}

func (s *FileSet) Delete(path string) {
	delete(s.files, path)
}

func (s *FileSet) Len() int {
	return len(s.files)
}

// Paths returns every path in sorted order.
func (s *FileSet) Paths() []string {
	out := make([]string, 0, len(s.files))
	for p := range s.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// IsBinary reports whether data looks binary: a NUL byte within the first
// 8000 bytes (git's heuristic).
func IsBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}
