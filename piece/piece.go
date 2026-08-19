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
	"github.com/Templetry/engine/catalog"
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
	// Identity renames the piece's canonical entity names with the piece's
	// own variables — the mechanism behind pieces per object.
	Identity []manifest.Rename `yaml:"identity,omitempty" json:"identity,omitempty"`
	Patches  []manifest.Patch  `yaml:"patches,omitempty" json:"patches,omitempty"`
	// AppliesTo lists the template names this piece supports (the `name`
	// of their template.yml). Empty means universal (ADR-0016).
	AppliesTo []string `yaml:"applies_to,omitempty" json:"applies_to,omitempty"`
}

// Supports reports whether the piece applies to a template by name.
func (m *Manifest) Supports(templateName string) bool {
	if len(m.AppliesTo) == 0 {
		return true
	}
	for _, n := range m.AppliesTo {
		if n == templateName {
			return true
		}
	}
	return false
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

// cutPieceRoot strips whichever piece root a path carries.
func cutPieceRoot(path string) (string, bool) {
	for _, root := range Roots {
		if rest, ok := strings.CutPrefix(path, root); ok {
			return rest, true
		}
	}
	return "", false
}

// FetchForm downloads the form a GitHub-sourced project was rendered
// from, returning its files and the resolved head commit.
func FetchForm(ans answers.Answers) (*source.FileSet, string, error) {
	src, ref, path, err := source.ParseSourceString(ans.Template.Source)
	if err != nil {
		return nil, "", err
	}
	files, err := source.Fetch(src, ref, path, "")
	if err != nil {
		return nil, "", err
	}
	commit := ""
	if sha, err := source.ResolveRef(src, ref, ""); err == nil {
		commit = sha
	}
	return files, commit, nil
}

// Roots are the directories a form may keep its pieces in. `_pieces/` is
// the escape hatch for glob-based toolchains: Go, and any tool that walks
// a tree, skips directories starting with an underscore, so the template
// still compiles in place with its pieces alongside (ADR-0003 + ADR-0014).
var Roots = []string{"pieces/", "_pieces/"}

// Info is one piece as listings show it.
type Info struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Applied     bool   `json:"applied"`
	Common      bool   `json:"common,omitempty"` // came from a shared repo
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
		rest, ok := cutPieceRoot(path)
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
	out := source.NewFileSet()
	for _, path := range formFiles.Paths() {
		for _, root := range Roots {
			if rest, ok := strings.CutPrefix(path, root+name+"/"); ok && rest != "" {
				out.Put(rest, formFiles.Get(path))
			}
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

// Resolved is a piece ready to be inspected or applied, wherever it lives.
type Resolved struct {
	Name     string
	Manifest *Manifest
	Files    *source.FileSet
	Source   string // what the answers file records for this piece
	Commit   string // the source's resolved head
	Common   bool   // true when it came from a shared repository
}

// FetchCommon downloads one common piece and checks it supports the
// template (ADR-0016).
func FetchCommon(entry catalog.CommonPiece, templateName string) (*Resolved, error) {
	src, err := source.ParseRef(entry.SourceRef())
	if err != nil {
		return nil, err
	}
	ref := entry.Ref
	if ref == "" {
		ref = "main"
	}
	files, err := source.Fetch(src, ref, entry.Path, "")
	if err != nil {
		return nil, err
	}
	pm, err := ManifestOf(files)
	if err != nil {
		return nil, fmt.Errorf("common piece %s: %w", entry.Name, err)
	}
	if !pm.Supports(templateName) {
		return nil, fmt.Errorf("piece %s does not apply to %s", entry.Name, templateName)
	}
	commit := ""
	if sha, err := source.ResolveRef(src, ref, ""); err == nil {
		commit = sha
	}
	name := entry.Name
	if name == "" {
		name = pm.Name
	}
	return &Resolved{
		Name: name, Manifest: pm, Files: files, Commit: commit, Common: true,
		Source: source.FormatSource(src, ref, entry.Path),
	}, nil
}

// Available lists everything a project can adopt: the pieces its form
// ships plus the common pieces of the registry that support its template.
// Form-local pieces win on a name clash (ADR-0016).
func Available(formFiles *source.FileSet, reg *catalog.Registry, ans answers.Answers) []Info {
	out := List(formFiles, ans)
	seen := map[string]bool{}
	for _, i := range out {
		seen[i.Name] = true
	}
	if reg == nil {
		return out
	}
	applied := map[string]bool{}
	for _, p := range ans.Pieces {
		applied[p.Name] = true
	}
	for _, entry := range reg.Pieces {
		if seen[entry.Name] {
			continue
		}
		res, err := FetchCommon(entry, ans.Template.Name)
		if err != nil {
			continue // incompatible or unreachable: simply not offered
		}
		seen[res.Name] = true
		out = append(out, Info{
			Name:        res.Name,
			Description: res.Manifest.Description,
			Applied:     applied[res.Name],
			Common:      true,
		})
	}
	return out
}

// Resolve finds one piece by name for a project: form-local first, then
// the registry's common pieces.
func Resolve(name string, formFiles *source.FileSet, formSource string,
	reg *catalog.Registry, ans answers.Answers) (*Resolved, error) {
	if files, err := Extract(formFiles, name); err == nil {
		pm, err := ManifestOf(files)
		if err != nil {
			return nil, err
		}
		src := formSource
		if src != "" && src != "local" {
			src += "/pieces/" + name
		}
		return &Resolved{Name: name, Manifest: pm, Files: files, Source: src}, nil
	}
	// Several entries may share a name with disjoint applies_to — one
	// implementation per ecosystem — so try them all and keep the one
	// that supports this template (ADR-0016).
	var lastErr error
	if reg != nil {
		for _, entry := range reg.Pieces {
			if entry.Name != name {
				continue
			}
			res, err := FetchCommon(entry, ans.Template.Name)
			if err == nil {
				return res, nil
			}
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no piece %q for this project (try 'templetry pieces')", name)
}

// Result is what Apply did: the piece-owned files written and the resolved
// piece variables — both destined for the answers record.
type Result struct {
	Files     []string
	Variables map[string]string
}

// ManifestOf reads the piece manifest out of an extracted piece FileSet.
func ManifestOf(pieceFiles *source.FileSet) (*Manifest, error) {
	f := pieceFiles.Get("piece.yml")
	if f == nil {
		f = pieceFiles.Get("piece.yaml")
	}
	if f == nil {
		return nil, fmt.Errorf("piece has no piece.yml")
	}
	return Load(f.Data)
}

// Render produces the piece's files as they would land in the project,
// without touching disk. Shared by Apply and by the update cycle.
func Render(formM *manifest.Manifest, pm *Manifest, pieceFiles *source.FileSet,
	projectVars, pieceVars map[string]string) (*source.FileSet, map[string]string, error) {
	// Resolve the piece's own variables with the manifest machinery.
	holder := &manifest.Manifest{Variables: pm.Variables}
	resolved, err := holder.Resolve(manifest.Inputs{Variables: pieceVars})
	if err != nil {
		return nil, nil, fmt.Errorf("piece %s: %w", pm.Name, err)
	}
	for k := range resolved.Variables {
		if _, clash := projectVars[k]; clash {
			return nil, nil, fmt.Errorf("piece %s: variable %q clashes with a template variable", pm.Name, k)
		}
	}
	plan, err := planner.BuildPiece(formM, projectVars, resolved.Variables, pm.Identity, pieceFiles)
	if err != nil {
		return nil, nil, err
	}
	rendered, err := render.Apply(plan, pieceFiles)
	if err != nil {
		return nil, nil, err
	}
	rendered.Delete(answers.Path) // a piece is not a project
	return rendered, resolved.Variables, nil
}

// Apply renders the piece with the form's identity over the project's
// recorded variables plus the piece's own, then writes it into the
// project. Piece files must not collide with existing paths (decoupling
// is enforced); declared patches are the only touch on shared files.
func Apply(projectDir string, formM *manifest.Manifest, pm *Manifest, pieceFiles *source.FileSet, projectVars, pieceVars map[string]string) (Result, error) {
	none := Result{}
	rendered, resolvedVars, err := Render(formM, pm, pieceFiles, projectVars, pieceVars)
	if err != nil {
		return none, err
	}

	// Enforced decoupling: no rendered path may already exist.
	for _, p := range rendered.Paths() {
		full, err := render.Contain(projectDir, p)
		if err != nil {
			return none, fmt.Errorf("piece %s: %w", pm.Name, err)
		}
		if _, err := os.Stat(full); err == nil {
			return none, fmt.Errorf("piece %s: %s already exists in the project — pieces may not overwrite files", pm.Name, p)
		}
	}
	written := []string{}
	for _, p := range rendered.Paths() {
		full, err := render.Contain(projectDir, p)
		if err != nil {
			return none, fmt.Errorf("piece %s: %w", pm.Name, err)
		}
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
	for k, v := range resolvedVars {
		merged[k] = v
	}
	for _, patch := range pm.Patches {
		full, err := render.Contain(projectDir, patch.File)
		if err != nil {
			return none, fmt.Errorf("piece %s: %w", pm.Name, err)
		}
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
	return Result{Files: written, Variables: resolvedVars}, nil
}
