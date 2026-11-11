package safepath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsGitName(t *testing.T) {
	for _, s := range []string{
		".git", ".GIT", ".Git", ".git ", ".git.", ".git. .", "GIT~1", "git~1", "git~12",
		".g" + string(rune(0x200c)) + "it", ".gi" + string(rune(0xfeff)) + "t", string(rune(0x200e)) + ".git",
	} {
		if !IsGitName(s) {
			t.Errorf("IsGitName(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		".gitignore", ".gitattributes", ".github", ".gitmodules", ".gitkeep", "git", "gitt~1",
		"git~", "git~a", "x.git", ".git-hooks", "a.git", "",
	} {
		if IsGitName(s) {
			t.Errorf("IsGitName(%q) = true, want false", s)
		}
	}
}

func TestValidateRelRejects(t *testing.T) {
	bad := map[string]string{
		"":                       "empty",
		"/etc/passwd":            "absolute",
		"../x":                   "climbs",
		"a/../../x":              "climbs",
		"..":                     "climbs",
		`..\x`:                   "backslash",
		`a\b`:                    "backslash",
		"C:/x":                   "colon",
		"x:y":                    "colon",
		".git/hooks/pre-commit":  "git directory",
		".GIT/hooks/pre-commit":  "git directory",
		".git":                   "git directory",
		"sub/.git/config":        "git directory",
		"sub/.git":               "git directory",
		".git /hooks/pre-commit": "git directory",
		".git./hooks/pre-commit": "git directory",
		"git~1/hooks/pre-commit": "git directory",
		"GIT~1/hooks/pre-commit": "git directory",
		"a/b/./.git/x":           "git directory",
		"a/.git/../.git/x":       "git directory",
		"a\x00b":                 "control",
		"a\nb":                   "control",
		"dir./file":              "dot or space",
		"dir /file":              "dot or space",
		"con":                    "reserved",
		"src/NUL.txt":            "reserved",
		"com1.log":               "reserved",
		"LPT9":                   "reserved",
	}
	for p, want := range bad {
		err := ValidateRel(p)
		if err == nil {
			t.Errorf("ValidateRel(%q) = nil, want an error mentioning %q", p, want)
		} else if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateRel(%q) = %v, want it to mention %q", p, err, want)
		}
	}
}

func TestValidateRelAllows(t *testing.T) {
	for _, p := range []string{
		"a.txt", "a/b/c.txt", "./a.txt", "a/../b.txt", ".gitignore", ".gitattributes",
		".github/workflows/ci.yml", "sub/.gitignore", ".gitmodules", "src/main.go", ".templetry-answers.yml",
		"docs/git.md", "x.git/y", "console.log", ".",
	} {
		if err := ValidateRel(p); err != nil {
			t.Errorf("ValidateRel(%q) = %v, want nil", p, err)
		}
	}
}

func TestValidateRelEscapesAreTyped(t *testing.T) {
	for _, p := range []string{"/x", "../x", `a\b`, "C:/x"} {
		if err := ValidateRel(p); !errors.Is(err, ErrEscape) {
			t.Errorf("ValidateRel(%q) = %v, want ErrEscape", p, err)
		}
	}
	if err := ValidateRel(".git/x"); errors.Is(err, ErrEscape) {
		t.Error(".git is refused, but it is not an escape")
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
}

func TestResolveRefusesSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "victim.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(root, "linkdir"))
	symlinkOrSkip(t, filepath.Join(outside, "victim.txt"), filepath.Join(root, "linkfile"))
	if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(root, "real", "nested"))

	for _, p := range []string{"linkdir/new.txt", "linkdir", "linkfile", "real/nested/deep/new.txt", "linkdir/sub/x"} {
		if _, err := Resolve(root, p); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Errorf("Resolve(%q) = %v, want a symlink refusal", p, err)
		}
	}
	for _, p := range []string{"real/ok.txt", "brand/new/dir/ok.txt", "ok.txt"} {
		if _, err := Resolve(root, p); err != nil {
			t.Errorf("Resolve(%q): %v", p, err)
		}
	}
}

// The root itself may be reached through a link (macOS temp dirs are):
// only what lies below it is inspected.
func TestResolveAllowsALinkedRoot(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "root-link")
	symlinkOrSkip(t, real, link)
	if _, err := Resolve(link, "a/b.txt"); err != nil {
		t.Errorf("a symlinked root must be fine: %v", err)
	}
}
