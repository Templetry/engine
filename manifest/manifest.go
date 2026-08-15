package manifest

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/goccy/go-yaml"
)

// Manifest is the parsed template.yml — Templetry's public API (ADR-0002).
type Manifest struct {
	SchemaVersion int    `yaml:"schema_version" json:"schema_version"`
	Name          string `yaml:"name" json:"name"`
	Description   string `yaml:"description,omitempty" json:"description,omitempty"`

	// The taxonomy (ADR-0017). Three independent axes, each a list, because
	// a form is usually more than one thing: fastapi-users is a backend and
	// a database schema; a KMP form is multiplatform and android and ios.
	Kinds      []string `yaml:"kinds,omitempty" json:"kinds,omitempty"`
	Languages  []string `yaml:"languages,omitempty" json:"languages,omitempty"`
	Frameworks []string `yaml:"frameworks,omitempty" json:"frameworks,omitempty"`

	// Deprecated: superseded by Kinds/Languages/Frameworks. Still parsed
	// because the manifest is public API and a field only disappears with a
	// major; ignored by filtering.
	Platform  string `yaml:"platform,omitempty" json:"platform,omitempty"`
	Framework string `yaml:"framework,omitempty" json:"framework,omitempty"`

	Variables []Variable `yaml:"variables,omitempty" json:"variables,omitempty"`
	Identity      []Rename   `yaml:"identity,omitempty" json:"identity,omitempty"`
	Features      []Feature  `yaml:"features,omitempty" json:"features,omitempty"`
	Presets       []Preset   `yaml:"presets,omitempty" json:"presets,omitempty"`
	Verify        *Verify    `yaml:"verify,omitempty" json:"verify,omitempty"`
}

type Variable struct {
	Key     string   `yaml:"key" json:"key"`
	Label   string   `yaml:"label,omitempty" json:"label,omitempty"`
	Type    string   `yaml:"type,omitempty" json:"type,omitempty"` // string (default) | select | boolean
	Pattern string   `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	Options []string `yaml:"options,omitempty" json:"options,omitempty"`
	Default *string  `yaml:"default,omitempty" json:"default,omitempty"`
}

type Rename struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
}

type Feature struct {
	Key       string   `yaml:"key" json:"key"`
	Label     string   `yaml:"label,omitempty" json:"label,omitempty"`
	Default   bool     `yaml:"default,omitempty" json:"default,omitempty"`
	Files     []string `yaml:"files,omitempty" json:"files,omitempty"`
	Patches   []Patch  `yaml:"patches,omitempty" json:"patches,omitempty"`
	Requires  []string `yaml:"requires,omitempty" json:"requires,omitempty"`
	Conflicts []string `yaml:"conflicts,omitempty" json:"conflicts,omitempty"`
}

// Preset is a named feature combination — one click instead of n toggles.
type Preset struct {
	Key      string          `yaml:"key" json:"key"`
	Label    string          `yaml:"label,omitempty" json:"label,omitempty"`
	Features map[string]bool `yaml:"features" json:"features"`
}

type Patch struct {
	File  string `yaml:"file" json:"file"`
	Op    string `yaml:"op" json:"op"` // add | replace | remove
	Path  string `yaml:"path" json:"path"`
	Value any    `yaml:"value,omitempty" json:"value,omitempty"`
}

type Verify struct {
	Image string `yaml:"image" json:"image"`
	Run   string `yaml:"run" json:"run"`
}

var (
	keyRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Kinds is the closed vocabulary of the taxonomy's primary axis (ADR-0017).
// Closed on purpose: a filter axis only works if two templates meaning the
// same thing use the same word, and a typo must not invent a tenth category.
// Adding a value is a deliberate act, recorded in the ADR.
var Kinds = []string{
	"frontend", "backend", "database", "infra",
	"multiplatform", "android", "ios", "desktop", "cli",
}

// ValidKind reports whether k belongs to the vocabulary.
func ValidKind(k string) bool { return contains(Kinds, k) }

// validateTaxonomy enforces ADR-0017: a closed vocabulary for kinds, and
// kebab-case shape for the open axes — which is what stops "C#", "csharp"
// and "c-sharp" from becoming three different languages.
func (m *Manifest) validateTaxonomy() error {
	seen := map[string]bool{}
	for _, k := range m.Kinds {
		if !ValidKind(k) {
			return fmt.Errorf("unknown kind %q (allowed: %v)", k, Kinds)
		}
		if seen[k] {
			return fmt.Errorf("duplicate kind %q", k)
		}
		seen[k] = true
	}
	for _, axis := range []struct {
		name   string
		values []string
	}{{"language", m.Languages}, {"framework", m.Frameworks}} {
		seen := map[string]bool{}
		for _, v := range axis.values {
			if !nameRe.MatchString(v) {
				return fmt.Errorf("%s %q must be kebab-case", axis.name, v)
			}
			if seen[v] {
				return fmt.Errorf("duplicate %s %q", axis.name, v)
			}
			seen[v] = true
		}
	}
	return nil
}

// Load parses and validates a template.yml document. A UTF-8 BOM (common
// from Windows editors) is tolerated — without stripping it, the first key
// silently fails to parse (study I §6).
func Load(data []byte) (*Manifest, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("template.yml: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("template.yml: %w", err)
	}
	return &m, nil
}

// Validate enforces the normative rules of the v1 manifest spec.
func (m *Manifest) Validate() error {
	if m.SchemaVersion != 1 {
		return fmt.Errorf("schema_version must be 1, got %d", m.SchemaVersion)
	}
	if !nameRe.MatchString(m.Name) {
		return fmt.Errorf("name %q must be kebab-case", m.Name)
	}
	if err := m.validateTaxonomy(); err != nil {
		return err
	}
	varKeys := map[string]bool{}
	for _, v := range m.Variables {
		if !keyRe.MatchString(v.Key) {
			return fmt.Errorf("variable key %q is invalid", v.Key)
		}
		if varKeys[v.Key] {
			return fmt.Errorf("duplicate variable key %q", v.Key)
		}
		varKeys[v.Key] = true
		switch v.Type {
		case "", "string":
			if v.Pattern != "" {
				if _, err := regexp.Compile(v.Pattern); err != nil {
					return fmt.Errorf("variable %q: invalid pattern: %v", v.Key, err)
				}
			}
		case "select":
			if len(v.Options) == 0 {
				return fmt.Errorf("variable %q: select needs options", v.Key)
			}
			if v.Default != nil && !contains(v.Options, *v.Default) {
				return fmt.Errorf("variable %q: default %q not in options", v.Key, *v.Default)
			}
		case "boolean":
			if v.Default != nil && *v.Default != "true" && *v.Default != "false" {
				return fmt.Errorf("variable %q: boolean default must be true or false", v.Key)
			}
		default:
			return fmt.Errorf("variable %q: unknown type %q", v.Key, v.Type)
		}
	}
	dummy := map[string]string{}
	for k := range varKeys {
		dummy[k] = "x"
	}
	for _, id := range m.Identity {
		if id.From == "" {
			return fmt.Errorf("identity entry with empty from")
		}
		if _, err := Expand(id.To, dummy); err != nil {
			return fmt.Errorf("identity %q: %v", id.From, err)
		}
	}
	featKeys := map[string]bool{}
	for _, f := range m.Features {
		if !keyRe.MatchString(f.Key) {
			return fmt.Errorf("feature key %q is invalid", f.Key)
		}
		if featKeys[f.Key] {
			return fmt.Errorf("duplicate feature key %q", f.Key)
		}
		featKeys[f.Key] = true
		for _, p := range f.Patches {
			if p.Op != "add" && p.Op != "replace" && p.Op != "remove" {
				return fmt.Errorf("feature %q: patch op %q must be add, replace or remove", f.Key, p.Op)
			}
			if p.File == "" || p.Path == "" {
				return fmt.Errorf("feature %q: patch needs file and path", f.Key)
			}
			if s, ok := p.Value.(string); ok {
				if _, err := Expand(s, dummy); err != nil {
					return fmt.Errorf("feature %q: patch value: %v", f.Key, err)
				}
			}
		}
	}
	for _, f := range m.Features {
		for _, req := range f.Requires {
			if !featKeys[req] {
				return fmt.Errorf("feature %q requires unknown feature %q", f.Key, req)
			}
			if req == f.Key {
				return fmt.Errorf("feature %q requires itself", f.Key)
			}
		}
		for _, con := range f.Conflicts {
			if !featKeys[con] {
				return fmt.Errorf("feature %q conflicts with unknown feature %q", f.Key, con)
			}
			if con == f.Key {
				return fmt.Errorf("feature %q conflicts with itself", f.Key)
			}
		}
	}
	presetKeys := map[string]bool{}
	for _, p := range m.Presets {
		if !keyRe.MatchString(p.Key) {
			return fmt.Errorf("preset key %q is invalid", p.Key)
		}
		if presetKeys[p.Key] {
			return fmt.Errorf("duplicate preset key %q", p.Key)
		}
		presetKeys[p.Key] = true
		for k := range p.Features {
			if !featKeys[k] {
				return fmt.Errorf("preset %q references unknown feature %q", p.Key, k)
			}
		}
	}
	if m.Verify != nil && (m.Verify.Image == "" || m.Verify.Run == "") {
		return fmt.Errorf("verify needs both image and run")
	}
	return nil
}

// Inputs are the raw user choices for one render.
type Inputs struct {
	Variables map[string]string
	Features  map[string]bool
	Preset    string
}

// Resolved are fully-resolved, validated inputs: every variable has a value
// and every feature has a state.
type Resolved struct {
	Variables map[string]string
	Features  map[string]bool
}

// Resolve applies defaults and validates user inputs against the manifest.
// Unknown keys are errors: typos never pass silently.
func (m *Manifest) Resolve(in Inputs) (*Resolved, error) {
	res := &Resolved{Variables: map[string]string{}, Features: map[string]bool{}}
	varDecl := map[string]Variable{}
	for _, v := range m.Variables {
		varDecl[v.Key] = v
	}
	for k := range in.Variables {
		if _, ok := varDecl[k]; !ok {
			return nil, fmt.Errorf("unknown variable %q", k)
		}
	}
	for _, v := range m.Variables {
		val, given := in.Variables[v.Key]
		if !given {
			if v.Default == nil {
				return nil, fmt.Errorf("variable %q has no value and no default", v.Key)
			}
			val = *v.Default
		}
		switch v.Type {
		case "", "string":
			if v.Pattern != "" {
				re := regexp.MustCompile(v.Pattern)
				if !re.MatchString(val) {
					return nil, fmt.Errorf("variable %q: value %q does not match pattern %q", v.Key, val, v.Pattern)
				}
			}
		case "select":
			if !contains(v.Options, val) {
				return nil, fmt.Errorf("variable %q: value %q not in options %v", v.Key, val, v.Options)
			}
		case "boolean":
			if val != "true" && val != "false" {
				return nil, fmt.Errorf("variable %q: value %q must be true or false", v.Key, val)
			}
		}
		res.Variables[v.Key] = val
	}
	featDecl := map[string]Feature{}
	for _, f := range m.Features {
		featDecl[f.Key] = f
	}
	for k := range in.Features {
		if _, ok := featDecl[k]; !ok {
			return nil, fmt.Errorf("unknown feature %q", k)
		}
	}
	// Resolution order: defaults, then the preset's states, then explicit
	// user choices — the more specific always wins.
	var preset *Preset
	if in.Preset != "" {
		for i := range m.Presets {
			if m.Presets[i].Key == in.Preset {
				preset = &m.Presets[i]
				break
			}
		}
		if preset == nil {
			return nil, fmt.Errorf("unknown preset %q", in.Preset)
		}
	}
	for _, f := range m.Features {
		state := f.Default
		if preset != nil {
			if s, ok := preset.Features[f.Key]; ok {
				state = s
			}
		}
		if s, given := in.Features[f.Key]; given {
			state = s
		}
		res.Features[f.Key] = state
	}
	// requires/conflicts are enforced on the final states — never silently
	// auto-enabled or auto-disabled (typos and surprises never pass).
	for _, f := range m.Features {
		if !res.Features[f.Key] {
			continue
		}
		for _, req := range f.Requires {
			if !res.Features[req] {
				return nil, fmt.Errorf("feature %q requires %q — enable it too, or disable %q", f.Key, req, f.Key)
			}
		}
		for _, con := range f.Conflicts {
			if res.Features[con] {
				return nil, fmt.Errorf("features %q and %q cannot be enabled together", f.Key, con)
			}
		}
	}
	return res, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
