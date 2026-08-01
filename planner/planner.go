package planner

import (
	"fmt"
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
		if reason, excluded := excludedBy(m, res.Features, path); excluded {
			plan.Files = append(plan.Files, ops.FileAction{Src: path, Action: ops.Exclude, Reason: reason})
			continue
		}
		dest := applyToPath(path, pathReps)
		if prev, dup := destOf[dest]; dup {
			return nil, fmt.Errorf("identity map sends both %q and %q to %q", prev, path, dest)
		}
		destOf[dest] = path

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
