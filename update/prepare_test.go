package update

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Templetry/engine/answers"
	"github.com/Templetry/engine/internal/forgetest"
	"github.com/Templetry/engine/manifest"
	"github.com/Templetry/engine/planner"
	"github.com/Templetry/engine/render"
	"github.com/Templetry/engine/source"
)

// These tests drive the whole update cycle the way a user meets it: a
// project is rendered from a template published on a (fake) forge at
// commit c1, the user edits it, the template moves on to c2, and Prepare
// plus Apply must bring the template's changes in without losing the
// user's.

const (
	repo      = "acme/templates"
	srcString = "github.com/acme/templates@main/demo"
)

const demoManifest = `schema_version: 1
name: demo
variables:
  - key: project_name
    type: string
identity:
  - from: "template-app"
    to: "{project_name.kebab}"
`

// form prefixes every path with the form's subdirectory in the repo.
func form(files map[string]string) map[string]string {
	out := map[string]string{"demo/template.yml": demoManifest}
	for p, d := range files {
		out["demo/"+p] = d
	}
	return out
}

// renderProject renders the template at main into a fresh directory, the
// same steps `templetry init` takes.
func renderProject(t *testing.T, vars map[string]string) string {
	t.Helper()
	src, ref, sub, err := source.ParseSourceString(srcString)
	if err != nil {
		t.Fatal(err)
	}
	files, err := source.Fetch(src, ref, sub, "")
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(files.Get("template.yml").Data)
	if err != nil {
		t.Fatal(err)
	}
	p, err := planner.Build(m, manifest.Inputs{Variables: vars}, files)
	if err != nil {
		t.Fatal(err)
	}
	p.Source = srcString
	if sha, err := source.ResolveRef(src, ref, ""); err == nil {
		p.SourceCommit = sha
	}
	out, err := render.Apply(p, files)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := render.WriteDir(out, dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readFile(t *testing.T, dir, p string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
	if err != nil {
		return "", false
	}
	return string(data), true
}

func writeFile(t *testing.T, dir, p, data string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	untouched = ""  // the user never edits the file
	deleted   = "-" // the user deletes the file
	absent    = "-" // the template (or the project after apply) has no such file
)

func TestPrepareAndApplyCycle(t *testing.T) {
	cases := []struct {
		name   string
		path   string // template path
		dest   string // project path, when identity renames it
		v1, v2 string // template content at c1 and c2 (absent = not shipped)
		user   string // what the user leaves on disk before updating
		status string // expected entry status; "unchanged" or "" (no entry)
		after  string // expected project content after Apply (absent = no file)
		check  func(t *testing.T, got string)
	}{
		{
			name: "unchanged on both sides", path: "README.md",
			v1: "# readme\n", v2: "# readme\n", user: untouched,
			status: "unchanged", after: "# readme\n",
		},
		{
			name: "user re-saved with CRLF only", path: "crlf.txt",
			v1: "a\nb\n", v2: "a\nb\n", user: "a\r\nb\r\n",
			status: "unchanged", after: "a\r\nb\r\n",
		},
		{
			name: "template-only change overwrites", path: "stale.txt",
			v1: "v1\n", v2: "v2\n", user: untouched,
			status: "modified", after: "v2\n",
		},
		{
			name: "user-only change survives", path: "notes.txt",
			v1: "one\ntwo\n", v2: "one\ntwo\n", user: "one\ntwo\nmine\n",
			// The template did not move, so the three-way merge is clean
			// and its result is exactly the user's file.
			status: "merged", after: "one\ntwo\nmine\n",
		},
		{
			name: "both changed different lines merge cleanly", path: "both.txt",
			v1: "l1\nl2\nl3\nl4\nl5\n", v2: "l1\nl2\nl3\nl4\nl5 template\n", user: "l1 user\nl2\nl3\nl4\nl5\n",
			status: "merged", after: "l1 user\nl2\nl3\nl4\nl5 template\n",
		},
		{
			name: "both changed the same line conflicts", path: "clash.txt",
			v1: "base\n", v2: "template\n", user: "yours\n",
			status: "conflict",
			check: func(t *testing.T, got string) {
				for _, want := range []string{"<<<<<<< yours", "yours\n", "=======", "template\n", ">>>>>>> template-new"} {
					if !strings.Contains(got, want) {
						t.Errorf("conflict file lacks %q:\n%s", want, got)
					}
				}
			},
		},
		{
			name: "user-deleted file is restored", path: "gone.txt",
			v1: "x\n", v2: "x\n", user: deleted,
			status: "added", after: "x\n",
		},
		{
			name: "template-deleted file stays in the project", path: "removed.txt",
			v1: "keep\n", v2: absent, user: untouched,
			status: "", after: "keep\n",
		},
		{
			name: "template rename: old path kept", path: "old-name.txt",
			v1: "same body\n", v2: absent, user: untouched,
			status: "", after: "same body\n",
		},
		{
			name: "template rename: new path added", path: "new-name.txt",
			v1: absent, v2: "same body\n", user: untouched,
			status: "added", after: "same body\n",
		},
		{
			name: "new template file", path: "fresh.txt",
			v1: absent, v2: "brand new\n", user: untouched,
			status: "added", after: "brand new\n",
		},
		{
			name: "user file the template never had", path: "mine.txt",
			v1: absent, v2: absent, user: "private\n",
			status: "", after: "private\n",
		},
		{
			name: "identity-renamed file follows the project name", path: "src/template-app.ts", dest: "src/my-app.ts",
			v1: "export const name = 'template-app'\n", v2: "export const name = 'template-app'\nexport const v = 2\n", user: untouched,
			status: "modified", after: "export const name = 'my-app'\nexport const v = 2\n",
		},
		{
			name: "binary changed only upstream is replaced", path: "logo.bin",
			v1: "PNG\x00\x01old", v2: "PNG\x00\x02new", user: untouched,
			status: "modified", after: "PNG\x00\x02new",
		},
		{
			name: "binary changed on both sides keeps the user's bytes", path: "icon.bin",
			v1: "ICO\x00base", v2: "ICO\x00template", user: "ICO\x00yours",
			status: "conflict", after: "ICO\x00yours",
		},
	}

	forge := forgetest.Install(t)
	v1, v2 := map[string]string{}, map[string]string{}
	for _, c := range cases {
		if c.v1 != absent {
			v1[c.path] = c.v1
		}
		if c.v2 != absent {
			v2[c.path] = c.v2
		}
	}
	forge.Publish(t, repo, "main", "c1c1c1c1c1", form(v1))
	dir := renderProject(t, map[string]string{"project_name": "My App"})

	dest := func(c int) string {
		if cases[c].dest != "" {
			return cases[c].dest
		}
		return cases[c].path
	}
	for i, c := range cases {
		switch c.user {
		case untouched:
		case deleted:
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(dest(i)))); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
		default:
			writeFile(t, dir, dest(i), c.user)
		}
	}
	forge.Publish(t, repo, "main", "c2c2c2c2c2", form(v2))

	p, err := Prepare(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Template != "demo" || p.OldCommit != "c1c1c1c1c1" || p.NewCommit != "c2c2c2c2c2" {
		t.Errorf("preview header = %q %q -> %q", p.Template, p.OldCommit, p.NewCommit)
	}
	status := map[string]string{}
	for _, e := range p.Entries {
		status[e.Path] = e.Status
		if e.Path == answers.Path {
			t.Errorf("the answers file must never be part of the diff")
		}
	}
	wantUnchanged := 0
	for i, c := range cases {
		if c.status == "unchanged" {
			wantUnchanged++
			if _, listed := status[dest(i)]; listed {
				t.Errorf("%s: %s listed as %q, want unchanged", c.name, dest(i), status[dest(i)])
			}
			continue
		}
		if status[dest(i)] != c.status {
			t.Errorf("%s: status of %s = %q, want %q", c.name, dest(i), status[dest(i)], c.status)
		}
	}
	if p.Unchanged != wantUnchanged {
		t.Errorf("unchanged = %d, want %d", p.Unchanged, wantUnchanged)
	}

	// Preview is read-only: nothing on disk moved yet.
	if got, _ := readFile(t, dir, "stale.txt"); got != "v1\n" {
		t.Errorf("Prepare wrote to disk: stale.txt = %q", got)
	}

	n, err := p.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if n != len(p.Entries) {
		t.Errorf("Apply wrote %d files, want %d", n, len(p.Entries))
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := readFile(t, dir, dest(i))
			if c.check != nil {
				c.check(t, got)
				return
			}
			if c.after == absent {
				if ok {
					t.Errorf("%s exists after apply", dest(i))
				}
				return
			}
			if !ok || got != c.after {
				t.Errorf("%s after apply = %q (exists=%v), want %q", dest(i), got, ok, c.after)
			}
		})
	}

	// Bookkeeping: the anchor moved, the recorded inputs survived.
	ans, err := ReadAnswers(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ans.Template.Commit != "c2c2c2c2c2" || ans.Template.Source != srcString || ans.Template.Name != "demo" {
		t.Errorf("answers template = %+v", ans.Template)
	}
	if ans.Variables["project_name"] != "My App" {
		t.Errorf("answers variables = %v", ans.Variables)
	}

	// A second update against the same head finds nothing left to bring in
	// except the files the user still disagrees with.
	again, err := Prepare(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.OldCommit != "c2c2c2c2c2" {
		t.Errorf("second preview starts from %q", again.OldCommit)
	}
	for _, e := range again.Entries {
		switch e.Path {
		case "notes.txt", "both.txt", "clash.txt", "icon.bin":
		default:
			t.Errorf("second update still touches %s (%s)", e.Path, e.Status)
		}
	}
}

func TestPrepareWithoutUsableBase(t *testing.T) {
	// With no base render, an upstream change cannot be told apart from a
	// user edit: the two-way fallback reports a conflict instead of
	// silently overwriting.
	for _, tc := range []struct {
		name   string
		commit string
	}{
		{"no recorded commit", ""},
		{"recorded commit no longer fetchable", "deadbeef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forge := forgetest.Install(t)
			forge.Publish(t, repo, "main", "c1", form(map[string]string{"a.txt": "one\n", "same.txt": "s\n"}))
			dir := renderProject(t, map[string]string{"project_name": "X"})
			ans, err := ReadAnswers(dir)
			if err != nil {
				t.Fatal(err)
			}
			ans.Template.Commit = tc.commit
			if err := answers.Write(dir, ans); err != nil {
				t.Fatal(err)
			}
			forge.Publish(t, repo, "main", "c2", form(map[string]string{"a.txt": "two\n", "same.txt": "s\n"}))

			p, err := Prepare(dir, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Entries) != 1 || p.Entries[0].Path != "a.txt" || p.Entries[0].Status != "conflict" {
				t.Errorf("entries = %+v", p.Entries)
			}
			if p.Unchanged != 1 {
				t.Errorf("unchanged = %d", p.Unchanged)
			}
		})
	}
}

const apiPiece = `schema_version: 1
name: api
description: API client
`

func TestPrepareCarriesAppliedPieces(t *testing.T) {
	forge := forgetest.Install(t)
	forge.Publish(t, repo, "main", "c1", form(map[string]string{
		"main.txt":                           "main\n",
		"pieces/api/piece.yml":               apiPiece,
		"pieces/api/src/template-app-api.ts": "export const v = 1 // template-app\n",
	}))
	dir := renderProject(t, map[string]string{"project_name": "Shop"})
	// The piece as `templetry add api` leaves it.
	writeFile(t, dir, "src/shop-api.ts", "export const v = 1 // shop\n")
	ans, err := ReadAnswers(dir)
	if err != nil {
		t.Fatal(err)
	}
	ans.Pieces = []answers.AppliedPiece{
		{Name: "api", Source: srcString + "/pieces/api", Commit: "c1", Files: []string{"src/shop-api.ts"}},
		// Removed upstream since: it simply stops updating.
		{Name: "ghost", Source: srcString + "/pieces/ghost", Commit: "c1"},
	}
	if err := answers.Write(dir, ans); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pieces")); err == nil {
		t.Fatal("the form's pieces directory leaked into the render")
	}

	forge.Publish(t, repo, "main", "c2", form(map[string]string{
		"main.txt":                           "main\n",
		"pieces/api/piece.yml":               apiPiece,
		"pieces/api/src/template-app-api.ts": "export const v = 2 // template-app\n",
	}))
	p, err := Prepare(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Entries) != 1 {
		t.Fatalf("entries = %+v", p.Entries)
	}
	if e := p.Entries[0]; e.Path != "src/shop-api.ts" || e.Status != "modified" || e.Piece != "api" {
		t.Errorf("piece entry = %+v", e)
	}
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if got, _ := readFile(t, dir, "src/shop-api.ts"); got != "export const v = 2 // shop\n" {
		t.Errorf("piece file after update = %q", got)
	}
	after, err := ReadAnswers(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Pieces) != 2 {
		t.Fatalf("pieces after update = %+v", after.Pieces)
	}
	for _, pc := range after.Pieces {
		if pc.Commit != "c2" {
			t.Errorf("piece %s anchor = %q, want c2", pc.Name, pc.Commit)
		}
	}
	if len(after.Pieces[0].Files) != 1 || after.Pieces[0].Files[0] != "src/shop-api.ts" {
		t.Errorf("piece files lost: %+v", after.Pieces[0])
	}
}

func TestPrepareErrors(t *testing.T) {
	forge := forgetest.Install(t)
	forge.Publish(t, repo, "main", "c1", form(map[string]string{"a.txt": "a\n"}))
	forge.Publish(t, "acme/nomanifest", "main", "c1", map[string]string{"demo/a.txt": "a\n"})
	forge.Publish(t, "acme/strict", "main", "c1", map[string]string{"demo/template.yml": `schema_version: 1
name: strict
variables:
  - key: project_name
    type: string
    pattern: "^[a-z]+$"
`})

	withAnswers := func(src string, vars map[string]string) string {
		dir := t.TempDir()
		a := answers.Answers{SchemaVersion: 1, Variables: vars}
		a.Template.Name = "demo"
		a.Template.Source = src
		if err := answers.Write(dir, a); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	cases := []struct {
		name, dir, want string
	}{
		{"no answers file", t.TempDir(), "no answers file"},
		{"rendered from a local directory", withAnswers("local", nil), "local directory"},
		{"unparseable source", withAnswers("no-at-sign", nil), "cannot parse source"},
		{"template gone from the forge", withAnswers("github.com/acme/missing@main/demo", nil), "HTTP 404"},
		{"template lost its manifest", withAnswers("github.com/acme/nomanifest@main/demo", nil), "template.yml"},
		{"recorded inputs no longer valid", withAnswers("github.com/acme/strict@main/demo", map[string]string{"project_name": "Not Valid"}), "project_name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Prepare(c.dir, "")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestFileContent(t *testing.T) {
	p := &Preview{
		Files:  fileset(map[string]string{"a.txt": "rendered\n"}),
		Merged: map[string][]byte{"b.txt": []byte("merged\n")},
	}
	for path, want := range map[string]string{"a.txt": "rendered\n", "b.txt": "merged\n"} {
		got, err := p.FileContent(path)
		if err != nil || string(got) != want {
			t.Errorf("FileContent(%s) = %q, %v", path, got, err)
		}
	}
	if _, err := p.FileContent("nope.txt"); err == nil {
		t.Error("FileContent of an unknown path must fail")
	}
}

func TestApplyRefusesPathsOutsideTheProject(t *testing.T) {
	dir := t.TempDir()
	p := &Preview{
		Dir:     dir,
		Entries: []Entry{{Path: "../escape.txt", Status: "added"}},
		Files:   fileset(map[string]string{"../escape.txt": "x"}),
	}
	if _, err := p.Apply(); err == nil {
		t.Fatal("Apply wrote outside the project")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); err == nil {
		t.Error("escape.txt was written")
	}
}

func TestApplyLeavesAnswersAloneWithoutANewCommit(t *testing.T) {
	dir := t.TempDir()
	a := answers.Answers{SchemaVersion: 1}
	a.Template.Name = "demo"
	a.Template.Commit = "old"
	if err := answers.Write(dir, a); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, answers.Path))
	p := &Preview{Dir: dir, Files: source.NewFileSet(), answers: a}
	if n, err := p.Apply(); err != nil || n != 0 {
		t.Fatalf("Apply = %d, %v", n, err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, answers.Path))
	if !bytes.Equal(before, after) {
		t.Errorf("answers rewritten without a resolved head:\n%s", after)
	}
}

func TestDiffAgainstDiskSkipsPathsOutsideTheProject(t *testing.T) {
	entries, unchanged, _ := diffAgainstDisk(t.TempDir(), fileset(map[string]string{"../outside.txt": "x"}), nil)
	if len(entries) != 0 || unchanged != 0 {
		t.Errorf("a path escaping the project was diffed: %+v", entries)
	}
}

func TestGitMergeFile(t *testing.T) {
	cases := []struct {
		name              string
		ours, base, their string
		conflicts         int
		want              string
	}{
		{"identical", "a\n", "a\n", "a\n", 0, "a\n"},
		{"only theirs moved", "a\n", "a\n", "b\n", 0, "b\n"},
		{"only ours moved", "b\n", "a\n", "a\n", 0, "b\n"},
		{"disjoint edits", "x\n2\n3\n4\n5\n", "1\n2\n3\n4\n5\n", "1\n2\n3\n4\ny\n", 0, "x\n2\n3\n4\ny\n"},
		{"two conflicting hunks", "x\n2\n3\n4\n5\n6\n7\n8\nx\n", "1\n2\n3\n4\n5\n6\n7\n8\n9\n", "y\n2\n3\n4\n5\n6\n7\n8\ny\n", 2, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, n, err := gitMergeFile([]byte(c.ours), []byte(c.base), []byte(c.their))
			if err != nil {
				t.Fatal(err)
			}
			if n != c.conflicts {
				t.Errorf("conflicts = %d, want %d", n, c.conflicts)
			}
			if c.want != "" && string(out) != c.want {
				t.Errorf("merge = %q, want %q", out, c.want)
			}
		})
	}
	t.Run("binary input is an error, not 255 conflicts", func(t *testing.T) {
		if _, _, err := gitMergeFile([]byte("a\x00b"), []byte("a\x00c"), []byte("a\x00d")); err == nil {
			t.Error("git cannot merge binary files; gitMergeFile must say so")
		}
	})
}
