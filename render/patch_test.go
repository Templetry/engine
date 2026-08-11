package render

import (
	"strings"
	"testing"

	"github.com/Templetry/engine/manifest"
)

func TestPatchYAML(t *testing.T) {
	doc := []byte("dependencies:\n  base: 1.0.0\nname: demo\n")
	p := manifest.Patch{File: "config.yml", Op: "add", Path: "/dependencies/router", Value: "^7.0.0"}
	out, err := applyPatch("config.yml", doc, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "router: ^7.0.0") {
		t.Errorf("yaml add missing:\n%s", out)
	}
	// Deterministic: same input, byte-identical output (NFR4).
	again, err := applyPatch("config.yml", doc, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(again) {
		t.Errorf("yaml patching is not deterministic:\n%s\nvs\n%s", out, again)
	}
}

func TestPatchTOML(t *testing.T) {
	doc := []byte("[project]\nname = \"demo\"\n\n[project.dependencies]\nbase = \"1.0\"\n")
	p := manifest.Patch{File: "pyproject.toml", Op: "add", Path: "/project/dependencies/fastapi", Value: "0.115"}
	out, err := applyPatch("pyproject.toml", doc, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `fastapi = "0.115"`) {
		t.Errorf("toml add missing:\n%s", out)
	}
	rm := manifest.Patch{File: "pyproject.toml", Op: "remove", Path: "/project/dependencies/base"}
	out2, err := applyPatch("pyproject.toml", out, rm, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out2), "base =") {
		t.Errorf("toml remove failed:\n%s", out2)
	}
}

func TestPatchUnsupportedExtension(t *testing.T) {
	p := manifest.Patch{File: "main.go", Op: "add", Path: "/x", Value: 1}
	if _, err := applyPatch("main.go", []byte("{}"), p, nil); err == nil {
		t.Error("non-structured target must be rejected")
	}
}
