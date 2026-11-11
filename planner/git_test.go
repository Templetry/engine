package planner

import (
	"strings"
	"testing"

	"github.com/Templetry/engine/manifest"
)

// A template that ships files under .git is refused at planning time, before
// any file is rendered: the plan never contains a git hook.
func TestBuildRejectsGitDirectories(t *testing.T) {
	m := fixtureManifest(t)
	in := manifest.Inputs{Variables: map[string]string{"base_package": "com.x"}}
	for _, p := range []string{".git/hooks/pre-commit", "x/.GIT/config", "git~1/hooks/pre-commit", ".git"} {
		_, err := Build(m, in, files("README.md", p))
		if err == nil || !strings.Contains(err.Error(), "git directory") {
			t.Errorf("template path %q must be rejected, got %v", p, err)
		}
	}
	if _, err := Build(m, in, files(".gitignore", ".gitattributes", ".github/workflows/ci.yml")); err != nil {
		t.Errorf(".gitignore, .gitattributes and .github must stay allowed: %v", err)
	}
}
