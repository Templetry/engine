package manifest

import "strings"

import "testing"

// base is a minimal valid manifest; each case adds only its taxonomy, so a
// failure can only come from the axis under test.
func base(taxonomy string) []byte {
	return []byte("schema_version: 1\nname: a-form\n" + taxonomy)
}

// `kinds` is a closed vocabulary on purpose (ADR-0017): a filter axis only
// works if two templates meaning the same thing use the same word, and a
// typo must not quietly invent a tenth category.
func TestKindsAreAClosedVocabulary(t *testing.T) {
	for _, k := range Kinds {
		if _, err := Load(base("kinds: [" + k + "]\n")); err != nil {
			t.Errorf("kind %q should be valid: %v", k, err)
		}
	}
	// Quoted, so each case is one value rather than YAML flow syntax —
	// `kinds: []` is an empty list, which is legitimately valid.
	for _, bad := range []string{"webapp", "front", "bbdd", "Backend", "back-end", ""} {
		_, err := Load(base("kinds: [\"" + bad + "\"]\n"))
		if err == nil {
			t.Errorf("kind %q should be rejected", bad)
			continue
		}
		if !strings.Contains(err.Error(), "allowed") {
			t.Errorf("the error for %q should name the allowed set, got: %v", bad, err)
		}
	}
}

// An empty list is not the same as an invalid value: a form may declare an
// axis and leave it empty.
func TestEmptyAxisIsValid(t *testing.T) {
	if _, err := Load(base("kinds: []\nlanguages: []\n")); err != nil {
		t.Errorf("empty axes should be valid: %v", err)
	}
}

func TestKindsRejectDuplicates(t *testing.T) {
	if _, err := Load(base("kinds: [backend, backend]\n")); err == nil {
		t.Error("a repeated kind should be an error")
	}
}

// The open axes constrain shape only — which is what keeps "C#", "csharp"
// and "c-sharp" from becoming three different languages.
func TestOpenAxesEnforceShape(t *testing.T) {
	ok := []string{
		"languages: [typescript]\n",
		"languages: [c-sharp, go]\n",
		"frameworks: [spring-boot]\n",
		"frameworks: [vite, react]\n",
	}
	for _, y := range ok {
		if _, err := Load(base(y)); err != nil {
			t.Errorf("%q should be valid: %v", y, err)
		}
	}
	bad := []string{
		"languages: [TypeScript]\n",
		"languages: [\"C#\"]\n",
		"frameworks: [spring_boot]\n",
		"frameworks: [-vite]\n",
		"languages: [go, go]\n",
	}
	for _, y := range bad {
		if _, err := Load(base(y)); err == nil {
			t.Errorf("%q should be rejected", y)
		}
	}
}

// A form is usually more than one thing, which is why the axes are lists.
func TestMultipleValuesPerAxis(t *testing.T) {
	m, err := Load(base("kinds: [multiplatform, android, ios, desktop]\nlanguages: [kotlin]\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(m.Kinds) != 4 {
		t.Errorf("want 4 kinds, got %v", m.Kinds)
	}
}

// The taxonomy is optional: a manifest without it is valid, it simply
// matches no filter.
func TestTaxonomyIsOptional(t *testing.T) {
	if _, err := Load(base("")); err != nil {
		t.Errorf("a manifest without a taxonomy should be valid: %v", err)
	}
}

// platform/framework are public API under the compatibility policy, so they
// keep parsing until a major even though nothing reads them any more.
func TestDeprecatedFieldsStillParse(t *testing.T) {
	m, err := Load(base("platform: backend\nframework: fastapi\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Platform != "backend" || m.Framework != "fastapi" {
		t.Errorf("deprecated fields should still be read, got %q/%q", m.Platform, m.Framework)
	}
	// And they must not leak into the new axes: guessing that "backend"
	// means kinds:[backend] would put labels on templates that never
	// claimed them, while looking like it worked.
	if len(m.Kinds) != 0 {
		t.Errorf("platform must not populate kinds, got %v", m.Kinds)
	}
}
