package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Templetry/engine/source"
)

func fileset(files map[string]string) *source.FileSet {
	fs := source.NewFileSet()
	for p, data := range files {
		fs.Put(p, &source.File{Data: []byte(data)})
	}
	return fs
}

func TestDiffAgainstDisk(t *testing.T) {
	dir := t.TempDir()
	write := func(p, data string) {
		t.Helper()
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("same.txt", "alpha\n")
	write("stale.txt", "old content\n")                       // untouched by user, template moved
	write("both.txt", "line1 user\nline2\nline3\n")           // user touched line1, template touches line3
	write("clash.txt", "user version\n")                      // both touched the same line

	rendered := fileset(map[string]string{
		"same.txt":  "alpha\n",
		"stale.txt": "new content\n",
		"both.txt":  "line1\nline2\nline3 template\n",
		"clash.txt": "template version\n",
		"fresh.txt": "brand new\n",
	})
	base := fileset(map[string]string{
		"same.txt":  "alpha\n",
		"stale.txt": "old content\n",
		"both.txt":  "line1\nline2\nline3\n",
		"clash.txt": "base version\n",
	})

	entries, unchanged, merged := diffAgainstDisk(dir, rendered, base)
	if unchanged != 1 {
		t.Errorf("unchanged = %d, want 1", unchanged)
	}
	got := map[string]string{}
	for _, e := range entries {
		got[e.Path] = e.Status
	}
	want := map[string]string{
		"fresh.txt": "added",
		"stale.txt": "modified",
		"both.txt":  "merged",
		"clash.txt": "conflict",
	}
	for p, status := range want {
		if got[p] != status {
			t.Errorf("%s = %q, want %q", p, got[p], status)
		}
	}
	if m, ok := merged["both.txt"]; !ok {
		t.Errorf("both.txt has no merge result")
	} else if string(m) != "line1 user\nline2\nline3 template\n" {
		t.Errorf("both.txt merge = %q", m)
	}
}

func TestApplyNeverDeletes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mine.txt"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Preview{
		Dir:     dir,
		Entries: []Entry{{Path: "new.txt", Status: "added"}},
		Files:   fileset(map[string]string{"new.txt": "hello\n"}),
	}
	n, err := p.Apply()
	if err != nil || n != 1 {
		t.Fatalf("apply: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mine.txt")); err != nil {
		t.Errorf("apply deleted an unrelated file")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "new.txt"))
	if string(data) != "hello\n" {
		t.Errorf("new.txt = %q", data)
	}
}
