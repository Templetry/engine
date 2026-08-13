// Package update implements the template update cycle for an already
// generated project: re-render its template at the current head with the
// recorded inputs, diff against the tree on disk, three-way merge where
// both sides moved, and apply. The desktop app and the CLI share this
// machinery (roadmap 1.x, lane A).
package update

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Templetry/engine/answers"
	"github.com/Templetry/engine/manifest"
	"github.com/Templetry/engine/planner"
	"github.com/Templetry/engine/render"
	"github.com/Templetry/engine/source"
)

// Answers is the provenance record every render writes (see the answers
// package, which owns the format).
type Answers = answers.Answers

// ReadAnswers loads a project's .templetry-answers.yml.
func ReadAnswers(dir string) (Answers, error) { return answers.Read(dir) }

// Entry is one file the update would touch.
type Entry struct {
	Path   string `json:"path"`
	Status string `json:"status"` // added | modified | merged | conflict
}

// Preview is a prepared update: what would change, plus the rendered
// content needed to apply it.
type Preview struct {
	Dir       string  `json:"dir"`
	Template  string  `json:"template"`
	OldCommit string  `json:"oldCommit"`
	NewCommit string  `json:"newCommit"`
	Entries   []Entry `json:"entries"`
	Unchanged int     `json:"unchanged"`

	Files  *source.FileSet   `json:"-"`
	Merged map[string][]byte `json:"-"`
}

// Prepare re-renders a project's template at its current head with the
// recorded inputs and diffs the result against the project on disk.
// Nothing is written. token may be empty (anonymous rate limits).
func Prepare(dir, token string) (*Preview, error) {
	ans, err := ReadAnswers(dir)
	if err != nil {
		return nil, err
	}
	src, ref, path, err := source.ParseSourceString(ans.Template.Source)
	if err != nil {
		return nil, err
	}

	files, err := source.Fetch(src, ref, path, token)
	if err != nil {
		return nil, err
	}
	rendered, sourceCommit, err := renderAt(files, ans, token, &src, ref)
	if err != nil {
		return nil, err
	}

	// Base render: the template at the recorded commit, same inputs — the
	// third band for real merges when both sides touched a file.
	var baseRendered *source.FileSet
	if ans.Template.Commit != "" {
		if baseFiles, err := source.Fetch(src, ans.Template.Commit, path, token); err == nil {
			baseRendered, _, _ = renderAt(baseFiles, ans, "", nil, "")
		}
	}

	out := &Preview{
		Dir: dir, Template: ans.Template.Name,
		OldCommit: ans.Template.Commit, NewCommit: sourceCommit,
		Files: rendered,
	}
	out.Entries, out.Unchanged, out.Merged = diffAgainstDisk(dir, rendered, baseRendered)
	return out, nil
}

// renderAt renders the template files with the recorded inputs. When src
// is given, the resolved commit is recorded in the plan.
func renderAt(files *source.FileSet, ans Answers, token string, src *source.Ref, ref string) (*source.FileSet, string, error) {
	mf := files.Get("template.yml")
	if mf == nil {
		mf = files.Get("template.yaml")
	}
	if mf == nil {
		return nil, "", fmt.Errorf("the template no longer has a template.yml")
	}
	m, err := manifest.Load(mf.Data)
	if err != nil {
		return nil, "", err
	}
	p, err := planner.Build(m, manifest.Inputs{Variables: ans.Variables, Features: ans.Features}, files)
	if err != nil {
		return nil, "", err
	}
	p.Source = ans.Template.Source
	if src != nil {
		if sha, err := source.ResolveRef(*src, ref, token); err == nil {
			p.SourceCommit = sha
		}
	}
	rendered, err := render.Apply(p, files)
	if err != nil {
		return nil, "", err
	}
	return rendered, p.SourceCommit, nil
}

func normEOL(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// diffAgainstDisk classifies every rendered file against the project tree:
// added (not on disk), unchanged, modified (safe overwrite — the user never
// touched it), or merged/conflict via git merge-file when both sides moved.
func diffAgainstDisk(dir string, rendered, baseRendered *source.FileSet) ([]Entry, int, map[string][]byte) {
	entries := []Entry{}
	unchanged := 0
	merged := map[string][]byte{}
	for _, rp := range rendered.Paths() {
		newData := normEOL(rendered.Get(rp).Data)
		current, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rp)))
		if err != nil {
			entries = append(entries, Entry{Path: rp, Status: "added"})
			continue
		}
		cur := normEOL(current)
		if bytes.Equal(cur, newData) {
			unchanged++
			continue
		}
		var base []byte
		if baseRendered != nil {
			if bf := baseRendered.Get(rp); bf != nil {
				base = normEOL(bf.Data)
			}
		}
		if base != nil && bytes.Equal(cur, base) {
			// User never touched it: safe overwrite.
			entries = append(entries, Entry{Path: rp, Status: "modified"})
			continue
		}
		m, conflicts, err := gitMergeFile(cur, base, newData)
		if err != nil {
			entries = append(entries, Entry{Path: rp, Status: "conflict"})
			continue
		}
		merged[rp] = m
		status := "merged"
		if conflicts > 0 {
			status = "conflict"
		}
		entries = append(entries, Entry{Path: rp, Status: status})
	}
	return entries, unchanged, merged
}

// gitMergeFile three-way merges via git merge-file; returns merged content
// and the number of conflicts.
func gitMergeFile(ours, base, theirs []byte) ([]byte, int, error) {
	tmp, err := os.MkdirTemp("", "templetry-merge")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(tmp)
	o, b, t := filepath.Join(tmp, "ours"), filepath.Join(tmp, "base"), filepath.Join(tmp, "theirs")
	if err := os.WriteFile(o, ours, 0o644); err != nil {
		return nil, 0, err
	}
	if err := os.WriteFile(b, base, 0o644); err != nil {
		return nil, 0, err
	}
	if err := os.WriteFile(t, theirs, 0o644); err != nil {
		return nil, 0, err
	}
	cmd := exec.Command("git", "merge-file", "-p", "-L", "yours", "-L", "template-old", "-L", "template-new", o, b, t)
	out, err := cmd.Output()
	conflicts := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() > 0 {
			conflicts = ee.ExitCode()
		} else {
			return nil, 0, err
		}
	}
	return out, conflicts, nil
}

// FileContent returns the updated content of one previewed file — the
// merge result when one exists, the fresh render otherwise.
func (p *Preview) FileContent(path string) ([]byte, error) {
	if m, ok := p.Merged[path]; ok {
		return m, nil
	}
	f := p.Files.Get(path)
	if f == nil {
		return nil, fmt.Errorf("no %s in the update preview", path)
	}
	return f.Data, nil
}

// Apply writes the previewed added/modified/merged files into the project.
// It never deletes anything; review the result with git.
func (p *Preview) Apply() (int, error) {
	written := 0
	for _, e := range p.Entries {
		data, err := p.FileContent(e.Path)
		if err != nil {
			continue
		}
		full := filepath.Join(p.Dir, filepath.FromSlash(e.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}
