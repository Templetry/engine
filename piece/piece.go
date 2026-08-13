// Package piece implements lazy pieces (ADR-0014): decoupled units a
// generated project adopts after creation, with their own variables,
// drift anchor and update cycle. Piece files may never collide with
// existing project paths; shared files are touched only through patches.
package piece

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Templetry/engine/answers"
	"github.com/Templetry/engine/manifest"
	"github.com/Templetry/engine/planner"
	"github.com/Templetry/engine/render"
	"github.com/Templetry/engine/source"
	"github.com/goccy/go-yaml"
)

// Manifest is the parsed piece.yml.
type Manifest struct {
	SchemaVersion int                 `yaml:"schema_version" json:"schema_version"`
	Name          string              `yaml:"name" json:"name"`
	Description   string              `yaml:"description,omitempty" json:"description,omitempty"`
	Variables     []manifest.Variable `yaml:"variables,omitempty" json:"variables,omitempty"`
	Patches       []manifest.Patch    `yaml:"patches,omitempty" json:"patches,omitempty"`
}

var (
	keyRe  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// Load parses and validates a piece.yml document.
func Load(data []byte) (*Manifest, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("piece.yml: %w", err)
	}
	if m.SchemaVersion != 1 {
		return nil, fmt.Errorf("piece.yml: schema_version must be 1, got %d", m.SchemaVersion)
	}
	if !nameRe.MatchString(m.Name) {
		return nil, fmt.Errorf("piece.yml: name %q must be kebab-case", m.Name)
	}
	seen := map[string]bool{}
	dummy := map[string]string{}
	for _, v := range m.Variables {
		if !keyRe.MatchString(v.Key) {
			return nil, fmt.Errorf("piece.yml: variable key %q is invalid", v.Key)
		}
		if seen[v.Key] {
			return nil, fmt.Errorf("piece.yml: duplicate variable key %q", v.Key)
		}
		seen[v.Key] = true
		dummy[v.Key] = "x"
	}
	for _, p := range m.Patches {
		if p.Op != "add" && p.Op != "replace" && p.Op != "remove" {
			return nil, fmt.Errorf("piece.yml: patch op %q must be add, replace or remove", p.Op)
		}
		if p.File == "" || p.Path == "" {
			return nil, fmt.Errorf("piece.yml: patch needs file and path")
		}
	}
	return &m, nil
}

// FetchForm downloads the form a GitHub-sourced project was rendered
// from, returning its files and the resolved head commit.
func FetchForm(ans answers.Answers) (*source.FileSet, string, error) {
	rest, ok := strings.CutPrefix(ans.Template.Source, "github.com/")
	if !ok {
		return nil, "", fmt.Errorf("project source %q is not a GitHub template", ans.Template.Source)
	}
	repo, right, ok := strings.Cut(rest, "@")
	if !ok {
		return nil, "", fmt.Errorf("cannot parse source %q", ans.Template.Source)
	}
	ref, path, _ := strings.Cut(right, "/")
	files, err := source.FetchGitHubTarball(repo, ref, path)
	if err != nil {
		return nil, "", err
	}
	commit := ""
	if sha, err := source.ResolveGitHubRef(repo, ref, ""); err == nil {
		commit = sha
	}
	return files, commit, nil
}

// Info is one piece as listings show it.
type Info struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Applied     bool   `json:"applied"`
}

// List finds the pieces a form ships (pieces/<name>/piece.yml inside the
// form's FileSet), marking the ones the answers file already records.
func List(formFiles *source.FileSet, ans answers.Answers) []Info {
	applied := map[string]bool{}
	for _, p := range ans.Pieces {
		applied[p.Name] = true
	}
	out := []Info{}
	seen := map[string]bool{}
	for _, path := range formFiles.Paths() {
		rest, ok := strings.CutPrefix(path, "pieces/")
		if !ok {
			continue
		}
		name, file, ok := strings.Cut(rest, "/")
		if !ok || seen[name] || (file != "piece.yml" && file != "piece.yaml") {
			continue
		}
		seen[name] = true
		info := Info{Name: name, Applied: applied[name]}
		if m, err := Load(formFiles.Get(path).Data); err == nil {
			info.Description = m.Description
		}
		out = append(out, info)
	}
	return out
}

// Extract returns the piece subtree of a form FileSet with the prefix
// stripped, or an error naming the available pieces.
func Extract(formFiles *source.FileSet, name string) (*source.FileSet, error) {
	prefix := "pieces/" + name + "/"
	out := source.NewFileSet()
	for _, path := range formFiles.Paths() {
		if rest, ok := strings.CutPrefix(path, prefix); ok && rest != "" {
			out.Put(rest, formFiles.Get(path))
		}
	}
	if out.Len() == 0 {
		available := []string{}
		for _, info := range List(formFiles, answers.Answers{}) {
			available = append(available, info.Name)
		}
		if len(available) == 0 {
			return nil, fmt.Errorf("this template ships no pieces")
		}
		return nil, fmt.Errorf("no piece %q (available: %s)", name, strings.Join(available, ", "))
	}
	if out.Get("piece.yml") == nil && out.Get("piece.yaml") == nil {
		return nil, fmt.Errorf("piece %q has no piece.yml", name)
	}
	return out, nil
}

// Result is what Apply did: the piece-owned files written and the resolved
// piece variables — both destined for the answers record.
type Result struct {
	Files     []string
	Variables map[string]string
}

// Apply renders the piece with the form's identity over the project's
// recorded variables plus the piece's own, then writes it into the
// project. Piece files must not collide with existing paths (decoupling
// is enforced); declared patches are the only touch on shared files.
func Apply(projectDir string, formM *manifest.Manifest, pm *Manifest, pieceFiles *source.FileSet, projectVars, pieceVars map[string]string) (Result, error) {
	none := Result{}

	// Resolve the piece's own variables with the manifest machinery.
	holder := &manifest.Manifest{Variables: pm.Variables}
	resolved, err := holder.Resolve(manifest.Inputs{Variables: pieceVars})
	if err != nil {
		return none, fmt.Errorf("piece %s: %w", pm.Name, err)
	}
	for k := range resolved.Variables {
		if _, clash := projectVars[k]; clash {
			return none, fmt.Errorf("piece %s: variable %q clashes with a template variable", pm.Name, k)
		}
	}

	plan, err := planner.BuildPiece(formM, projectVars, resolved.Variables, pieceFiles)
	if err != nil {
		return none, err
	}
	rendered, err := render.Apply(plan, pieceFiles)
	if err != nil {
		return none, err
	}
	rendered.Delete(answers.Path) // a piece is not a project

	// Enforced decoupling: no rendered path may already exist.
	for _, p := range rendered.Paths() {
		full := filepath.Join(projectDir, filepath.FromSlash(p))
		if _, err := os.Stat(full); err == nil {
			return none, fmt.Errorf("piece %s: %s already exists in the project — pieces may not overwrite files", pm.Name, p)
		}
	}
	written := []string{}
	for _, p := range rendered.Paths() {
		full := filepath.Join(projectDir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return none, err
		}
		mode := os.FileMode(0o644)
		if rendered.Get(p).Exec {
			mode = 0o755
		}
		if err := os.WriteFile(full, rendered.Get(p).Data, mode); err != nil {
			return none, err
		}
		written = append(written, p)
	}

	// Wiring: declared patches against existing project files.
	merged := map[string]string{}
	for k, v := range projectVars {
		merged[k] = v
	}
	for k, v := range resolved.Variables {
		merged[k] = v
	}
	for _, patch := range pm.Patches {
		full := filepath.Join(projectDir, filepath.FromSlash(patch.File))
		doc, err := os.ReadFile(full)
		if err != nil {
			return none, fmt.Errorf("piece %s: patch target %s: %w", pm.Name, patch.File, err)
		}
		out, err := render.PatchFile(patch.File, doc, patch, merged)
		if err != nil {
			return none, fmt.Errorf("piece %s: patch %s %s: %w", pm.Name, patch.Op, patch.Path, err)
		}
		if err := os.WriteFile(full, out, 0o644); err != nil {
			return none, err
		}
	}
	return Result{Files: written, Variables: resolved.Variables}, nil
}
