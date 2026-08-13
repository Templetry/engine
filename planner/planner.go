package planner

import (
	"fmt"
	pathpkg "path"
	"sort"
	"strings"

	"github.com/Templetry/engine/manifest"
	"github.com/Templetry/engine/ops"
	"github.com/Templetry/engine/source"
	"github.com/bmatcuk/doublestar/v4"
)

// manifestFiles are never part of the output.
var manifestFiles = map[string]bool{"template.yml": true, "template.yaml": true}

// Build turns (manifest, inputs, template files) into a Plan. Pure: no
// filesystem, no network, no clock.
func Build(m *manifest.Manifest, in manifest.Inputs, files *source.FileSet) (*ops.Plan, error) {
	res, err := m.Resolve(in)
	if err != nil {
		return nil, err
	}

	contentReps, pathReps, err := replacements(m, res.Variables)
	if err != nil {
		return nil, err
	}

	plan := &ops.Plan{
		Template:     m.Name,
		Variables:    res.Variables,
		Features:     res.Features,
		Replacements: contentReps,
	}

	patchTargets := map[string][]manifest.Patch{}
	for _, ft := range m.Features {
		if !res.Features[ft.Key] {
			continue
		}
		for _, p := range ft.Patches {
			patchTargets[p.File] = append(patchTargets[p.File], p)
		}
	}

	destOf := map[string]string{} // dest -> src, collision detection
	for _, path := range files.Paths() {
		if manifestFiles[path] {
			plan.Files = append(plan.Files, ops.FileAction{Src: path, Action: ops.Exclude, Reason: "manifest"})
			continue
		}
		if strings.HasPrefix(path, "pieces/") {
			// Pieces are applied post-creation (ADR-0014), never part of
			// the base render.
			plan.Files = append(plan.Files, ops.FileAction{Src: path, Action: ops.Exclude, Reason: "piece"})
			continue
		}
		if reason, excluded := excludedBy(m, res.Features, path); excluded {
			plan.Files = append(plan.Files, ops.FileAction{Src: path, Action: ops.Exclude, Reason: reason})
			continue
		}
		dest := applyToPath(path, pathReps)
		if err := validateDest(dest); err != nil {
			return nil, fmt.Errorf("identity map produces unsafe path %q for %q: %w", dest, path, err)
		}
		// Collision detection is case-insensitive: outputs must survive
		// Windows and macOS filesystems, where Same.txt and same.txt alias.
		destKey := strings.ToLower(dest)
		if prev, dup := destOf[destKey]; dup {
			return nil, fmt.Errorf("identity map sends both %q and %q to %q", prev, path, dest)
		}
		destOf[destKey] = path

		action := ops.Render
		if files.Get(path).Binary {
			action = ops.Copy
		}
		fa := ops.FileAction{Src: path, Dest: dest, Action: action}
		if ps, ok := patchTargets[path]; ok {
			if action != ops.Render {
				return nil, fmt.Errorf("patch target %q is a binary file", path)
			}
			fa.Patches = ps
			delete(patchTargets, path)
		}
		plan.Files = append(plan.Files, fa)
	}
	for file := range patchTargets {
		return nil, fmt.Errorf("patch target %q does not exist in the template (or is excluded)", file)
	}
	return plan, nil
}

// BuildPiece plans a piece render (ADR-0014): every piece file except its
// manifest, renamed through the FORM's identity map expanded with the
// project's recorded variables. The plan's variable set additionally holds
// the piece's own resolved variables so tpl:var directives in piece files
// can use both. Features are empty by design — tpl:if inside a piece is an
// error (a piece is its own unit).
func BuildPiece(formM *manifest.Manifest, projectVars, pieceVars map[string]string, files *source.FileSet) (*ops.Plan, error) {
	contentReps, pathReps, err := replacements(formM, projectVars)
	if err != nil {
		return nil, err
	}
	merged := map[string]string{}
	for k, v := range projectVars {
		merged[k] = v
	}
	for k, v := range pieceVars {
		merged[k] = v
	}
	plan := &ops.Plan{
		Template:     formM.Name,
		Variables:    merged,
		Features:     map[string]bool{},
		Replacements: contentReps,
	}
	destOf := map[string]string{}
	for _, path := range files.Paths() {
		if path == "piece.yml" || path == "piece.yaml" {
			plan.Files = append(plan.Files, ops.FileAction{Src: path, Action: ops.Exclude, Reason: "manifest"})
			continue
		}
		dest := applyToPath(path, pathReps)
		if err := validateDest(dest); err != nil {
			return nil, fmt.Errorf("identity map produces unsafe path %q for %q: %w", dest, path, err)
		}
		destKey := strings.ToLower(dest)
		if prev, dup := destOf[destKey]; dup {
			return nil, fmt.Errorf("identity map sends both %q and %q to %q", prev, path, dest)
		}
		destOf[destKey] = path
		action := ops.Render
		if files.Get(path).Binary {
			action = ops.Copy
		}
		plan.Files = append(plan.Files, ops.FileAction{Src: path, Dest: dest, Action: action})
	}
	return plan, nil
}

// replacements expands the identity map. Dotted identities additionally get a
// slash variant — applied to paths (so "com.template.base" renames
// "com/template/base" directories) AND to content (docs and scripts that
// reference those paths). Both lists are ordered longest-from first
// (deterministic, no substring shadowing), then lexicographically for equal
// lengths.
func replacements(m *manifest.Manifest, vars map[string]string) (content, paths []ops.Replacement, err error) {
	for _, id := range m.Identity {
		to, err := manifest.Expand(id.To, vars)
		if err != nil {
			return nil, nil, fmt.Errorf("identity %q: %v", id.From, err)
		}
		content = append(content, ops.Replacement{From: id.From, To: to})
		paths = append(paths, ops.Replacement{From: id.From, To: to})
		if strings.Contains(id.From, ".") {
			slash := ops.Replacement{
				From: strings.ReplaceAll(id.From, ".", "/"),
				To:   strings.ReplaceAll(to, ".", "/"),
			}
			content = append(content, slash)
			paths = append(paths, slash)
		}
	}
	order := func(list []ops.Replacement) {
		sort.SliceStable(list, func(i, j int) bool {
			if len(list[i].From) != len(list[j].From) {
				return len(list[i].From) > len(list[j].From)
			}
			return list[i].From < list[j].From
		})
	}
	order(content)
	order(paths)
	return content, paths, nil
}

// reservedSegments are Windows device names: illegal as a path segment's
// base name (with or without extension) on the engine's primary platform.
var reservedSegments = func() map[string]bool {
	m := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}
	for i := 1; i <= 9; i++ {
		m[fmt.Sprintf("COM%d", i)] = true
		m[fmt.Sprintf("LPT%d", i)] = true
	}
	return m
}()

// validateDest rejects output paths that could escape the output root or
// abuse platform path semantics — the defense against hostile third-party
// templates (study III hardening item).
func validateDest(dest string) error {
	if dest == "" {
		return fmt.Errorf("empty path")
	}
	if strings.ContainsAny(dest, "\\:") {
		return fmt.Errorf("backslash or colon not allowed")
	}
	for _, r := range dest {
		if r < 0x20 {
			return fmt.Errorf("control character not allowed")
		}
	}
	cleaned := pathpkg.Clean(dest)
	if pathpkg.IsAbs(cleaned) {
		return fmt.Errorf("absolute path not allowed")
	}
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("path escapes the output root")
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg != strings.TrimRight(seg, ". ") {
			return fmt.Errorf("segment %q ends in a dot or space (silently trimmed on Windows)", seg)
		}
		base := seg
		if i := strings.IndexByte(seg, '.'); i >= 0 {
			base = seg[:i]
		}
		if reservedSegments[strings.ToUpper(base)] {
			return fmt.Errorf("segment %q is a reserved Windows device name", seg)
		}
	}
	return nil
}

func applyToPath(path string, reps []ops.Replacement) string {
	for _, r := range reps {
		path = strings.ReplaceAll(path, r.From, r.To)
	}
	return path
}

// excludedBy implements the spec rule: a file is excluded when it matches the
// files globs of at least one inactive feature and no active feature's globs.
// Globs match template paths, not renamed paths.
func excludedBy(m *manifest.Manifest, states map[string]bool, path string) (string, bool) {
	inactive := ""
	for _, ft := range m.Features {
		for _, g := range ft.Files {
			ok, err := doublestar.Match(g, path)
			if err != nil || !ok {
				continue
			}
			if states[ft.Key] {
				return "", false
			}
			inactive = ft.Key
		}
	}
	if inactive != "" {
		return fmt.Sprintf("feature %s off", inactive), true
	}
	return "", false
}
