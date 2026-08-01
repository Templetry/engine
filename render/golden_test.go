package render_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Templetry/engine/manifest"
	"github.com/Templetry/engine/planner"
	"github.com/Templetry/engine/render"
	"github.com/Templetry/engine/source"
)

// TestGoldenReactMini is the end-to-end contract: fixed inputs over the
// react-mini fixture must produce the byte-exact golden tree (NFR4).
func TestGoldenReactMini(t *testing.T) {
	dir := filepath.Join("testdata", "react-mini")
	data, err := os.ReadFile(filepath.Join(dir, "template.yml"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	files, err := source.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.Build(m, manifest.Inputs{
		Variables: map[string]string{"project_name": "Demo Shop"},
		Features:  map[string]bool{"router": true},
	}, files)
	if err != nil {
		t.Fatal(err)
	}
	out, err := render.Apply(plan, files)
	if err != nil {
		t.Fatal(err)
	}

	golden, err := source.LoadDir(filepath.Join("testdata", "react-mini.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out.Paths(), golden.Paths()) {
		t.Fatalf("path sets differ:\n got: %v\nwant: %v", out.Paths(), golden.Paths())
	}
	for _, p := range golden.Paths() {
		want := bytes.ReplaceAll(golden.Get(p).Data, []byte("\r\n"), []byte("\n"))
		got := out.Get(p).Data
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs:\n--- got ---\n%s\n--- want ---\n%s", p, got, want)
		}
	}
}

// TestGoldenDeterminism renders twice and demands identical bytes (NFR4).
func TestGoldenDeterminism(t *testing.T) {
	dir := filepath.Join("testdata", "react-mini")
	data, _ := os.ReadFile(filepath.Join(dir, "template.yml"))
	m, err := manifest.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	files, err := source.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := manifest.Inputs{Variables: map[string]string{"project_name": "Demo Shop"}}
	run := func() *source.FileSet {
		p, err := planner.Build(m, in, files)
		if err != nil {
			t.Fatal(err)
		}
		out, err := render.Apply(p, files)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	a, b := run(), run()
	if !slices.Equal(a.Paths(), b.Paths()) {
		t.Fatal("non-deterministic path set")
	}
	for _, p := range a.Paths() {
		if !bytes.Equal(a.Get(p).Data, b.Get(p).Data) {
			t.Errorf("non-deterministic bytes for %s", p)
		}
	}
}
