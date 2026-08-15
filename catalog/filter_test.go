package catalog

import "testing"

var forms = map[string]Form{
	"go-cli":       {Form: "cli", Kinds: []string{"cli"}, Languages: []string{"go"}},
	"go-rest":      {Form: "rest-sqlite", Kinds: []string{"backend", "database"}, Languages: []string{"go"}, Frameworks: []string{"sqlite"}},
	"react":        {Form: "react-spa", Kinds: []string{"frontend"}, Languages: []string{"typescript"}, Frameworks: []string{"react", "vite"}},
	"untaxonomied": {Form: "legacy"},
}

// OR within an axis, AND across them (ADR-0017). Getting this backwards is
// the classic faceted-filter bug: it looks right on one-axis queries and
// returns nothing the moment two are combined.
func TestFilterCombinesAxes(t *testing.T) {
	cases := []struct {
		name   string
		filter Filter
		form   string
		want   bool
	}{
		{"empty filter takes everything", Filter{}, "go-cli", true},
		{"empty filter takes untagged forms too", Filter{}, "untaxonomied", true},

		{"single kind hits", Filter{Kinds: []string{"cli"}}, "go-cli", true},
		{"single kind misses", Filter{Kinds: []string{"cli"}}, "react", false},
		{"a form matches on its second kind", Filter{Kinds: []string{"database"}}, "go-rest", true},

		{"two kinds are OR", Filter{Kinds: []string{"cli", "frontend"}}, "react", true},
		{"two kinds are OR, other side", Filter{Kinds: []string{"cli", "frontend"}}, "go-cli", true},
		{"two kinds still exclude", Filter{Kinds: []string{"cli", "frontend"}}, "go-rest", false},

		{"axes are AND", Filter{Kinds: []string{"cli"}, Languages: []string{"go"}}, "go-cli", true},
		{"axes are AND: kind hits, language does not",
			Filter{Kinds: []string{"cli"}, Languages: []string{"rust"}}, "go-cli", false},
		{"axes are AND: language hits, kind does not",
			Filter{Kinds: []string{"frontend"}, Languages: []string{"go"}}, "go-cli", false},

		{"framework axis", Filter{Frameworks: []string{"vite"}}, "react", true},
		{"all three", Filter{Kinds: []string{"frontend"}, Languages: []string{"typescript"}, Frameworks: []string{"react"}}, "react", true},

		// A form that declares nothing claims nothing, so it matches no
		// filter. Matching everything would put untagged forms in every
		// result, which is worse than absent.
		{"untagged matches no filter", Filter{Kinds: []string{"cli"}}, "untaxonomied", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.filter.Match(forms[c.form]); got != c.want {
				t.Errorf("Match(%s) = %v, want %v", c.form, got, c.want)
			}
		})
	}
}

func TestFilterEmpty(t *testing.T) {
	if !(Filter{}).Empty() {
		t.Error("zero filter should be empty")
	}
	if (Filter{Frameworks: []string{"vite"}}).Empty() {
		t.Error("a filter with one axis set is not empty")
	}
}

// The taxonomy is authored by hand in two files, so case drift is a real
// risk; matching is case-insensitive to absorb it.
func TestFilterIgnoresCase(t *testing.T) {
	if !(Filter{Kinds: []string{"CLI"}}).Match(forms["go-cli"]) {
		t.Error("filter values should match regardless of case")
	}
}

// Registry v2.2 is additive: a document written before the taxonomy existed
// must still parse, with empty axes.
func TestParseAcceptsRegistryWithoutTaxonomy(t *testing.T) {
	data := []byte(`{"schema_version":2,"parents":[{"key":"go","repo":"Templetry/go","ref":"main",
		"forms":[{"form":"cli","name":"go-cli","path":"cli","status":"ready"}]}]}`)
	reg, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	f := reg.Parents[0].Forms[0]
	if len(f.Kinds) != 0 || len(f.Languages) != 0 || len(f.Frameworks) != 0 {
		t.Errorf("expected no taxonomy, got %+v", f)
	}
}
