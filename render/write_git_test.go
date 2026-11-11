package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Templetry/engine/source"
)

func setOf(paths ...string) *source.FileSet {
	s := source.NewFileSet()
	for _, p := range paths {
		s.Put(p, &source.File{Data: []byte("#!/bin/sh\necho pwned\n"), Exec: true})
	}
	return s
}

// Regression: a template that ships .git/hooks/pre-commit used to be written
// as is, and git runs that hook on the next commit by the project's owner.
func TestWriteDirRefusesGitHooks(t *testing.T) {
	for _, p := range []string{
		".git/hooks/pre-commit", ".GIT/hooks/pre-commit", ".Git/config", "git~1/hooks/pre-commit",
		".git /hooks/pre-commit", ".git./hooks/pre-commit", "vendor/lib/.git/hooks/pre-commit", ".git",
	} {
		root := t.TempDir()
		err := WriteDir(setOf("README.md", p), root)
		if err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("WriteDir with %q = %v, want a refusal", p, err)
		}
		entries, _ := os.ReadDir(root)
		if len(entries) != 0 {
			t.Errorf("%q: the destination must be untouched on refusal, found %d entries", p, len(entries))
		}
	}
}

func TestWriteDirKeepsGitAdjacentNames(t *testing.T) {
	root := t.TempDir()
	set := setOf(".gitignore", ".gitattributes", ".github/workflows/ci.yml", "sub/.gitignore", ".gitmodules")
	if err := WriteDir(set, root); err != nil {
		t.Fatal(err)
	}
	for _, p := range set.Paths() {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
			t.Errorf("%s was not written: %v", p, err)
		}
	}
}

func TestWriteDirRefusesAbsoluteAndTraversal(t *testing.T) {
	for _, p := range []string{"/tmp/evil", "../evil", "a/../../evil", `C:\evil`, "x/../.git/hooks/h"} {
		root := t.TempDir()
		if err := WriteDir(setOf(p), root); err == nil {
			t.Errorf("WriteDir with %q must refuse", p)
		}
	}
}

func TestWriteDirWritesThroughNoSymlink(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	err := WriteDir(setOf("link/pwned.sh"), root)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("WriteDir through a symlinked directory = %v, want a refusal", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.sh")); err == nil {
		t.Fatal("a file landed outside the root through a symlink")
	}

	// A symlink planted at the file's own name is refused too: WriteFile
	// would follow it and overwrite its target.
	target := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "file.txt")); err != nil {
		t.Skip(err)
	}
	if err := WriteDir(setOf("file.txt"), root); err == nil {
		t.Error("writing onto a symlink must be refused")
	}
	if data, _ := os.ReadFile(target); string(data) != "original" {
		t.Errorf("the symlink target was overwritten: %q", data)
	}
}

// One bad path among many must not leave the others half written.
func TestWriteDirIsAllOrNothing(t *testing.T) {
	root := t.TempDir()
	if err := WriteDir(setOf("a.txt", "b/c.txt", "z/.git/hooks/x"), root); err == nil {
		t.Fatal("expected a refusal")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Errorf("partial write: %d entries on disk", len(entries))
	}
}
