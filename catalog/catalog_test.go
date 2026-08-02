package catalog

import (
	"strings"
	"testing"
)

const sample = `{
  "schema_version": 2,
  "parents": [
    {
      "key": "kmp", "label": "Kotlin Multiplatform",
      "repo": "Templetry/kmp", "ref": "main",
      "forms": [
        {"form": "modular-features", "name": "kmp-modular-features", "path": "modular-features", "status": "ready"},
        {"form": "modular-ui", "name": "kmp-modular-ui", "path": "modular-ui", "status": "planned"}
      ]
    }
  ]
}`

func TestParseAndResolve(t *testing.T) {
	r, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	p, f, err := r.Resolve("kmp/modular-features")
	if err != nil {
		t.Fatal(err)
	}
	if p.Repo != "Templetry/kmp" || f.Path != "modular-features" {
		t.Errorf("unexpected resolution: %+v %+v", p, f)
	}
}

func TestResolveErrors(t *testing.T) {
	r, _ := Parse([]byte(sample))
	cases := []struct{ ref, wantErr string }{
		{"kmp/modular-ui", "planned"},
		{"kmp/nope", `no form "nope"`},
		{"web/anything", `no parent "web"`},
		{"just-one-token", "<parent>/<form>"},
	}
	for _, c := range cases {
		_, _, err := r.Resolve(c.ref)
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("Resolve(%q): got %v, want containing %q", c.ref, err, c.wantErr)
		}
	}
}

func TestParseBadSchema(t *testing.T) {
	if _, err := Parse([]byte(`{"schema_version": 1, "parents": []}`)); err == nil {
		t.Error("schema_version 1 must be rejected")
	}
}
