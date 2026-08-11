package manifest

import (
	"strings"
	"testing"
)

const valid = `
schema_version: 1
name: react-mini
variables:
  - key: project_name
    type: string
    pattern: "^[A-Za-z][A-Za-z0-9 ]*$"
  - key: node_version
    type: select
    options: ["20", "22"]
    default: "22"
identity:
  - from: "template-app"
    to: "{project_name.kebab}"
features:
  - key: router
    default: false
    files: ["src/routes/**"]
verify:
  image: node:22
  run: npm ci && npm run build
`

func TestLoadValid(t *testing.T) {
	m, err := Load([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "react-mini" || len(m.Variables) != 2 || len(m.Features) != 1 {
		t.Errorf("unexpected parse result: %+v", m)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct{ name, replace, with, wantErr string }{
		{"bad schema", "schema_version: 1", "schema_version: 2", "schema_version"},
		{"bad name", "name: react-mini", "name: React Mini", "kebab-case"},
		{"select without options", "options: [\"20\", \"22\"]\n    default: \"22\"", "default: \"22\"", "needs options"},
		{"identity unknown var", "{project_name.kebab}", "{nope.kebab}", "unknown variable"},
	}
	for _, c := range cases {
		doc := strings.Replace(valid, c.replace, c.with, 1)
		_, err := Load([]byte(doc))
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: got err %v, want containing %q", c.name, err, c.wantErr)
		}
	}
}

func TestResolve(t *testing.T) {
	m, err := Load([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.Resolve(Inputs{Variables: map[string]string{"project_name": "Demo Shop"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Variables["node_version"] != "22" {
		t.Error("default not applied")
	}
	if res.Features["router"] != false {
		t.Error("feature default not applied")
	}
	if _, err := m.Resolve(Inputs{}); err == nil {
		t.Error("missing required variable should error")
	}
	if _, err := m.Resolve(Inputs{Variables: map[string]string{"project_name": "9bad"}}); err == nil {
		t.Error("pattern violation should error")
	}
	if _, err := m.Resolve(Inputs{
		Variables: map[string]string{"project_name": "Ok"},
		Features:  map[string]bool{"nope": true},
	}); err == nil {
		t.Error("unknown feature should error")
	}
}

func TestLoadStripsBOM(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("schema_version: 1\nname: demo\n")...)
	m, err := Load(data)
	if err != nil {
		t.Fatalf("BOM manifest must load: %v", err)
	}
	if m.Name != "demo" {
		t.Errorf("name = %q, want demo", m.Name)
	}
}

func TestRequiresConflictsValidation(t *testing.T) {
	_, err := Load([]byte("schema_version: 1\nname: demo\nfeatures:\n  - key: a\n    requires: [ghost]\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown feature") {
		t.Errorf("unknown requires must fail, got %v", err)
	}
	_, err = Load([]byte("schema_version: 1\nname: demo\nfeatures:\n  - key: a\n    conflicts: [a]\n"))
	if err == nil || !strings.Contains(err.Error(), "conflicts with itself") {
		t.Errorf("self conflict must fail, got %v", err)
	}
}

func TestRequiresConflictsResolution(t *testing.T) {
	m, err := Load([]byte(`
schema_version: 1
name: demo
features:
  - key: auth
  - key: firebase
  - key: room
    requires: [auth]
  - key: sqlite
    conflicts: [room]
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Resolve(Inputs{Features: map[string]bool{"room": true}}); err == nil ||
		!strings.Contains(err.Error(), `requires "auth"`) {
		t.Errorf("unmet requires must fail, got %v", err)
	}
	if _, err := m.Resolve(Inputs{Features: map[string]bool{"room": true, "auth": true}}); err != nil {
		t.Errorf("met requires must pass, got %v", err)
	}
	if _, err := m.Resolve(Inputs{Features: map[string]bool{"room": true, "auth": true, "sqlite": true}}); err == nil ||
		!strings.Contains(err.Error(), "cannot be enabled together") {
		t.Errorf("conflict must fail, got %v", err)
	}
}

func TestPresets(t *testing.T) {
	m, err := Load([]byte(`
schema_version: 1
name: demo
features:
  - key: a
    default: true
  - key: b
  - key: c
presets:
  - key: minimal
    features: {a: false, b: true}
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Resolve(Inputs{Preset: "ghost"}); err == nil || !strings.Contains(err.Error(), "unknown preset") {
		t.Errorf("unknown preset must fail, got %v", err)
	}
	// defaults -> preset -> explicit: c stays default false, preset flips
	// a/b, the explicit choice overrides the preset for b.
	res, err := m.Resolve(Inputs{Preset: "minimal", Features: map[string]bool{"b": false}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"a": false, "b": false, "c": false}
	for k, v := range want {
		if res.Features[k] != v {
			t.Errorf("feature %s = %v, want %v", k, res.Features[k], v)
		}
	}
	_, err = Load([]byte("schema_version: 1\nname: demo\npresets:\n  - key: p\n    features: {ghost: true}\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown feature") {
		t.Errorf("preset with unknown feature must fail, got %v", err)
	}
}
