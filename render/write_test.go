package render

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestContainRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	escapes := []string{
		"../outside.txt",
		"a/../../outside.txt",
		"..",
	}
	if runtime.GOOS == "windows" {
		escapes = append(escapes, `..\outside.txt`)
	}
	for _, p := range escapes {
		if _, err := Contain(root, p); err == nil {
			t.Errorf("Contain(%q) must refuse, got nil", p)
		} else if !strings.Contains(err.Error(), "refusing to write outside") {
			t.Errorf("Contain(%q): unexpected error %v", p, err)
		}
	}
}

func TestContainAllowsInside(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"a.txt", "a/b/c.txt", "./a.txt", "a/../b.txt"} {
		full, err := Contain(root, p)
		if err != nil {
			t.Fatalf("Contain(%q): %v", p, err)
		}
		rel, err := filepath.Rel(root, full)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("Contain(%q) escaped: %q", p, full)
		}
	}
}

// A path that escapes must be refused before anything lands on disk, not
// after — an error returned mid-write still leaves the file written.
func TestContainWritesNothingOnRefusal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "escaped.txt")
	_ = os.Remove(outside)

	if _, err := Contain(root, "../escaped.txt"); err == nil {
		t.Fatal("expected refusal")
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatalf("%s was created", outside)
	}
}
