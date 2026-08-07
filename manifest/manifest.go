package manifest

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/goccy/go-yaml"
)

// Manifest is the parsed template.yml — Templetry's public API (ADR-0002).
type Manifest struct {
	SchemaVersion int        `yaml:"schema_version" json:"schema_version"`
	Name          string     `yaml:"name" json:"name"`
	Description   string     `yaml:"description,omitempty" json:"description,omitempty"`
	Platform      string     `yaml:"platform,omitempty" json:"platform,omitempty"`
	Framework     string     `yaml:"framework,omitempty" json:"framework,omitempty"`
	Variables     []Variable `yaml:"variables,omitempty" json:"variables,omitempty"`
	Identity      []Rename   `yaml:"identity,omitempty" json:"identity,omitempty"`
	Features      []Feature  `yaml:"features,omitempty" json:"features,omitempty"`
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
	Key     string   `yaml:"key" json:"key"`
	Label   string   `yaml:"label,omitempty" json:"label,omitempty"`
	Default bool     `yaml:"default,omitempty" json:"default,omitempty"`
	Files   []string `yaml:"files,omitempty" json:"files,omitempty"`
	Patches []Patch  `yaml:"patches,omitempty" json:"patches,omitempty"`
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
	if m.Verify != nil && (m.Verify.Image == "" || m.Verify.Run == "") {
		return fmt.Errorf("verify needs both image and run")
	}
	return nil
}

// Inputs are the raw user choices for one render.
type Inputs struct {
	Variables map[string]string
	Features  map[string]bool
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
	for _, f := range m.Features {
		state, given := in.Features[f.Key]
		if !given {
			state = f.Default
		}
		res.Features[f.Key] = state
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
