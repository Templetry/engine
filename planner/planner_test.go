package planner

import (
	"strings"
	"testing"

	"github.com/Templetry/engine/manifest"
	"github.com/Templetry/engine/ops"
	"github.com/Templetry/engine/source"
)

func fixtureManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load([]byte(`
schema_version: 1
name: demo
variables:
  - key: base_package
    type: string
features:
  - key: db
    default: false
    files: ["core/db/**"]
identity:
  - from: "com.template.base"
    to: "{base_package}"
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func files(paths ...string) *source.FileSet {
	fs := source.NewFileSet()
	for _, p := range paths {
		fs.Put(p, &source.File{Data: []byte("x")})
	}
	return fs
}

func TestBuild(t *testing.T) {
	m := fixtureManifest(t)
	fs := files(
		"template.yml",
		"core/db/Dao.kt",
		"src/com/template/base/App.kt",
		"README.md",
	)
	p, err := Build(m, manifest.Inputs{Variables: map[string]string{"base_package": "com.acme.shop"}}, fs)
	if err != nil {
		t.Fatal(err)
	}
	bySrc := map[string]ops.FileAction{}
	for _, f := range p.Files {
		bySrc[f.Src] = f
	}
	if bySrc["template.yml"].Action != ops.Exclude {
		t.Error("manifest must be excluded")
	}
	if fa := bySrc["core/db/Dao.kt"]; fa.Action != ops.Exclude || !strings.Contains(fa.Reason, "db") {
		t.Errorf("inactive feature file must be excluded, got %+v", fa)
	}
	if fa := bySrc["src/com/template/base/App.kt"]; fa.Dest != "src/com/acme/shop/App.kt" {
		t.Errorf("dotted identity must rename paths, got dest %q", fa.Dest)
	}
	if bySrc["README.md"].Action != ops.Render {
		t.Error("text file must be rendered")
	}
	hasSlash := false
	for _, r := range p.Replacements {
		if r.From == "com/template/base" && r.To == "com/acme/shop" {
			hasSlash = true
		}
	}
	if !hasSlash {
		t.Error("dotted identity must add a slash variant to content replacements")
	}
}

func TestFeatureOnIncludes(t *testing.T) {
	m := fixtureManifest(t)
	fs := files("core/db/Dao.kt")
	p, err := Build(m, manifest.Inputs{
		Variables: map[string]string{"base_package": "com.acme.shop"},
		Features:  map[string]bool{"db": true},
	}, fs)
	if err != nil {
		t.Fatal(err)
	}
	if p.Files[0].Action != ops.Render {
		t.Errorf("active feature file must be included, got %+v", p.Files[0])
	}
}

func TestDestCollision(t *testing.T) {
	m, err := manifest.Load([]byte(`
schema_version: 1
name: demo
variables:
  - key: name
    type: string
identity:
  - from: "old-a"
    to: "{name.kebab}"
  - from: "old-b"
    to: "{name.kebab}"
`))
	if err != nil {
		t.Fatal(err)
	}
	fs := files("old-a.txt", "old-b.txt")
	_, err = Build(m, manifest.Inputs{Variables: map[string]string{"name": "same"}}, fs)
	if err == nil || !strings.Contains(err.Error(), "identity map sends both") {
		t.Errorf("collision must error, got %v", err)
	}
}

func TestUnsafeDestRejected(t *testing.T) {
	for _, evil := range []string{"../{name.kebab}", "/abs/{name.kebab}", "C:{name.kebab}", "a\\\\b{name.kebab}"} {
		m, err := manifest.Load([]byte(`
schema_version: 1
name: demo
variables:
  - key: name
    type: string
identity:
  - from: "victim"
    to: "` + evil + `"
`))
		if err != nil {
			t.Fatalf("%s: %v", evil, err)
		}
		_, err = Build(m, manifest.Inputs{Variables: map[string]string{"name": "x"}}, files("victim.txt"))
		if err == nil || !strings.Contains(err.Error(), "unsafe path") {
			t.Errorf("identity to %q must be rejected, got %v", evil, err)
		}
	}
}

func TestWindowsHostileDestRejected(t *testing.T) {
	cases := []struct{ to, file, want string }{
		{"nul", "victim.txt", "reserved Windows device name"}, // nul.txt
		{"com1", "victim", "reserved Windows device name"},
		{"docs/aux", "victim.md", "reserved Windows device name"}, // docs/aux.md
		{"x.", "victim", "dot or space"},
		{"x ", "victim", "dot or space"},
	}
	for _, c := range cases {
		m, err := manifest.Load([]byte(`
schema_version: 1
name: demo
identity:
  - from: "victim"
    to: "` + c.to + `"
`))
		if err != nil {
			t.Fatalf("%s: %v", c.to, err)
		}
		_, err = Build(m, manifest.Inputs{}, files(c.file))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("identity to %q on %q must be rejected with %q, got %v", c.to, c.file, c.want, err)
		}
	}
}

func TestCaseInsensitiveDestCollision(t *testing.T) {
	m, err := manifest.Load([]byte(`
schema_version: 1
name: demo
identity:
  - from: "old-a"
    to: "Same"
  - from: "old-b"
    to: "same"
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Build(m, manifest.Inputs{}, files("old-a.txt", "old-b.txt"))
	if err == nil || !strings.Contains(err.Error(), "identity map sends both") {
		t.Errorf("case-insensitive collision must error (Same.txt vs same.txt alias on Windows/macOS), got %v", err)
	}
}

func TestPatchTargetMissing(t *testing.T) {
	m, err := manifest.Load([]byte(`
schema_version: 1
name: demo
features:
  - key: router
    default: true
    patches:
      - file: package.json
        op: add
        path: /x
        value: 1
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Build(m, manifest.Inputs{}, files("README.md"))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing patch target must error, got %v", err)
	}
}
