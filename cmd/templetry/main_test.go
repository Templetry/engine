package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Templetry/engine/internal/fakedocker"
	"github.com/Templetry/engine/internal/forgetest"
)

func TestMain(m *testing.M) {
	fakedocker.Main()
	os.Exit(m.Run())
}

const demoManifest = `schema_version: 1
name: demo
variables:
  - key: project_name
    type: string
    pattern: "^[A-Za-z][A-Za-z0-9 ]*$"
identity:
  - from: "template-app"
    to: "{project_name.kebab}"
features:
  - key: docs
    label: Docs folder
    default: false
    files: ["docs/**"]
verify:
  image: alpine:3
  run: test -f README.md
`

func demoFiles(readme string) map[string]string {
	return map[string]string{
		"template.yml":           demoManifest,
		"README.md":              readme,
		"src/template-app.go":    "package main // template-app\n",
		"docs/guide.md":          "guide\n",
		"pieces/ci/piece.yml":    "schema_version: 1\nname: ci\ndescription: CI workflow\n",
		"pieces/ci/ci/build.txt": "build template-app\n",
	}
}

// writeTree materializes files under a fresh directory.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, data := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// cli runs one command line and returns its exit code and both streams.
func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// remoteFixture publishes the demo form on a fake forge and returns a
// registry file pointing at it.
func remoteFixture(t *testing.T) (*forgetest.Forge, string) {
	t.Helper()
	forge := forgetest.Install(t)
	publishDemo(t, forge, "1111111aaaa", "# template-app\n")
	reg := filepath.Join(t.TempDir(), "registry.json")
	data := `{"schema_version": 2, "parents": [
	  {"key": "acme", "label": "Acme", "repo": "acme/templates", "ref": "main", "forms": [
	    {"form": "demo", "name": "demo", "path": "forms/demo", "status": "ready", "description": "Demo form",
	     "kinds": ["cli"], "languages": ["go"]},
	    {"form": "bare", "name": "bare", "path": "forms/bare", "description": "No taxonomy"}
	  ]}
	]}`
	if err := os.WriteFile(reg, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return forge, reg
}

func publishDemo(t *testing.T, forge *forgetest.Forge, commit, readme string) {
	t.Helper()
	files := map[string]string{}
	for p, d := range demoFiles(readme) {
		files["forms/demo/"+p] = d
	}
	forge.Publish(t, "acme/templates", "main", commit, files)
}

func TestDispatch(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{name: "no command", args: nil, code: 2, stdout: "usage:"},
		{name: "help", args: []string{"help"}, code: 0, stdout: "templetry update [dir]"},
		{name: "--help", args: []string{"--help"}, code: 0, stdout: "usage:"},
		{name: "version", args: []string{"version"}, code: 0, stdout: "templetry " + version + "\n"},
		{name: "unknown command", args: []string{"frobnicate"}, code: 2, stdout: "usage:", stderr: `unknown command "frobnicate"`},
		{name: "subcommand -h", args: []string{"plan", "-h"}, code: 0, stderr: "-template"},
		{name: "unknown flag", args: []string{"render", "--nope"}, code: 2, stderr: "flag provided but not defined: -nope"},
		{name: "plan needs a template", args: []string{"plan"}, code: 1, stderr: "error: --template is required"},
		{name: "render needs --out", args: []string{"render", "--template", "x"}, code: 1, stderr: "error: --template and --out are required"},
		{name: "verify needs a template", args: []string{"verify"}, code: 1, stderr: "error: --template is required"},
		{name: "init needs a reference", args: []string{"init", "--out", "x"}, code: 1, stderr: "usage: templetry init"},
		{name: "init needs --out", args: []string{"init", "acme/demo"}, code: 1, stderr: "error: --out is required"},
		{name: "add needs a piece", args: []string{"add"}, code: 1, stderr: "usage: templetry add"},
		{name: "list rejects an unknown kind", args: []string{"list", "--kind", "spaceship"}, code: 1, stderr: `unknown kind "spaceship"`},
		{name: "update outside a project", args: []string{"update", t.TempDir()}, code: 1, stderr: "error: no answers file"},
		{name: "pieces outside a project", args: []string{"pieces", t.TempDir()}, code: 1, stderr: "error: no answers file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, stdout, stderr := cli(t, c.args...)
			if code != c.code {
				t.Errorf("exit = %d, want %d (stdout %q, stderr %q)", code, c.code, stdout, stderr)
			}
			if !strings.Contains(stdout, c.stdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout, c.stdout)
			}
			if !strings.Contains(stderr, c.stderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, c.stderr)
			}
			if c.stderr == "" && stderr != "" {
				t.Errorf("unexpected stderr %q", stderr)
			}
		})
	}
}

func TestPlanAndRenderLocalTemplate(t *testing.T) {
	tpl := writeTree(t, demoFiles("# template-app\n"))

	code, stdout, stderr := cli(t, "plan", "--template", tpl, "--set", "project_name=Hello World")
	if code != 0 || !strings.Contains(stdout, "src/template-app.go -> src/hello-world.go") ||
		!strings.Contains(stdout, "exclude docs/guide.md (feature docs off)") {
		t.Fatalf("plan: %d %q %q", code, stdout, stderr)
	}

	code, stdout, _ = cli(t, "plan", "--template", tpl, "--set", "project_name=Hello World", "--feature", "docs", "--json")
	var plan struct {
		Template string          `json:"template"`
		Features map[string]bool `json:"features"`
	}
	if code != 0 || json.Unmarshal([]byte(stdout), &plan) != nil || plan.Template != "demo" || !plan.Features["docs"] {
		t.Errorf("plan --json: %d %q", code, stdout)
	}

	for _, bad := range []struct {
		args []string
		want string
	}{
		{[]string{"--set", "novalue"}, "--set wants key=value"},
		{[]string{"--set", "project_name=X", "--feature", "docs=maybe"}, "value must be true or false"},
		{[]string{"--set", "project_name=X", "--feature", "=true"}, "--feature wants key"},
		{[]string{"--set", "project_name=1 starts with a digit"}, "project_name"},
	} {
		code, _, stderr := cli(t, append([]string{"plan", "--template", tpl}, bad.args...)...)
		if code != 1 || !strings.Contains(stderr, bad.want) {
			t.Errorf("plan %v: exit %d, stderr %q, want %q", bad.args, code, stderr, bad.want)
		}
	}

	out := filepath.Join(t.TempDir(), "proj")
	code, stdout, stderr = cli(t, "render", "--template", tpl, "--out", out, "--set", "project_name=Hello World")
	if code != 0 || !strings.Contains(stdout, "rendered demo -> "+out) {
		t.Fatalf("render: %d %q %q", code, stdout, stderr)
	}
	if got := read(t, filepath.Join(out, "src", "hello-world.go")); got != "package main // hello-world\n" {
		t.Errorf("rendered file = %q", got)
	}
	for _, leaked := range []string{"template.yml", "docs", "pieces"} {
		if _, err := os.Stat(filepath.Join(out, leaked)); err == nil {
			t.Errorf("%s must not be rendered", leaked)
		}
	}
	if ans := read(t, filepath.Join(out, ".templetry-answers.yml")); !strings.Contains(ans, "project_name: Hello World") {
		t.Errorf("answers = %s", ans)
	}

	code, _, stderr = cli(t, "render", "--template", tpl, "--out", out, "--set", "project_name=Hello World")
	if code != 1 || !strings.Contains(stderr, "is not empty (use --force)") {
		t.Errorf("render into a non-empty dir: %d %q", code, stderr)
	}
	code, _, stderr = cli(t, "render", "--template", tpl, "--out", out, "--set", "project_name=Hello World", "--feature", "docs", "--force")
	if code != 0 {
		t.Errorf("render --force: %d %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "docs", "guide.md")); err != nil {
		t.Errorf("--feature docs did not render docs: %v", err)
	}

	// A local render has no remote to update from.
	code, _, stderr = cli(t, "update", out)
	if code != 1 || !strings.Contains(stderr, "local directory") {
		t.Errorf("update of a local render: %d %q", code, stderr)
	}

	// Its pieces are still reachable through --template.
	code, stdout, _ = cli(t, "pieces", out, "--template", tpl, "--registry", filepath.Join(t.TempDir(), "none.json"))
	if code != 0 || !strings.Contains(stdout, "ci") || !strings.Contains(stdout, "CI workflow") {
		t.Errorf("pieces --template: %d %q", code, stdout)
	}
	code, stdout, stderr = cli(t, "add", "ci", out, "--template", tpl, "--registry", filepath.Join(t.TempDir(), "none.json"))
	if code != 0 || !strings.Contains(stdout, "piece ci applied: 1 files") {
		t.Errorf("add --template: %d %q %q", code, stdout, stderr)
	}
	if got := read(t, filepath.Join(out, "ci", "build.txt")); got != "build hello-world\n" {
		t.Errorf("piece file = %q", got)
	}
	code, _, stderr = cli(t, "add", "ci", out, "--template", tpl)
	if code != 1 || !strings.Contains(stderr, "already applied") {
		t.Errorf("adding a piece twice: %d %q", code, stderr)
	}
}

func TestVerify(t *testing.T) {
	tpl := writeTree(t, demoFiles("# template-app\n"))

	t.Run("renders to a temp dir and runs the manifest's command", func(t *testing.T) {
		logPath := fakedocker.Install(t, 0)
		code, stdout, stderr := cli(t, "verify", "--template", tpl, "--set", "project_name=Hello World", "--keep")
		if code != 0 || !strings.Contains(stdout, "verify: test -f README.md in alpine:3") || !strings.Contains(stdout, "verify: OK") {
			t.Fatalf("verify: %d %q %q", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "fake docker: stdout") || !strings.Contains(stderr, "fake docker: stderr") {
			t.Errorf("container output must stream through: %q %q", stdout, stderr)
		}
		call := fakedocker.Last(t, logPath)
		if !strings.Contains(strings.Join(call.Files, " "), "src/hello-world.go") {
			t.Errorf("the container did not see the render: %v", call.Files)
		}
		kept := strings.TrimSpace(strings.SplitN(strings.TrimPrefix(stdout, "render kept at "), "\n", 2)[0])
		if _, err := os.Stat(filepath.Join(kept, "README.md")); err != nil {
			t.Errorf("--keep must leave the render at %q: %v", kept, err)
		} else {
			os.RemoveAll(kept)
		}
	})

	t.Run("an existing directory with --dir", func(t *testing.T) {
		logPath := fakedocker.Install(t, 0)
		existing := writeTree(t, map[string]string{"README.md": "mine\n"})
		if code, _, stderr := cli(t, "verify", "--template", tpl, "--dir", existing); code != 0 {
			t.Fatalf("verify --dir: %d %q", code, stderr)
		}
		if call := fakedocker.Last(t, logPath); len(call.Files) != 1 || call.Files[0] != "README.md" {
			t.Errorf("container saw %v", call.Files)
		}
	})

	t.Run("a failing container fails the command", func(t *testing.T) {
		fakedocker.Install(t, 1)
		code, stdout, stderr := cli(t, "verify", "--template", tpl, "--set", "project_name=X")
		if code != 1 || !strings.Contains(stderr, "error: verify failed") || strings.Contains(stdout, "verify: OK") {
			t.Errorf("verify with a failing container: %d %q %q", code, stdout, stderr)
		}
	})

	t.Run("a template without verify", func(t *testing.T) {
		files := demoFiles("x\n")
		files["template.yml"] = strings.Split(demoManifest, "verify:")[0]
		code, _, stderr := cli(t, "verify", "--template", writeTree(t, files))
		if code != 1 || !strings.Contains(stderr, "declares no verify") {
			t.Errorf("verify without a verify section: %d %q", code, stderr)
		}
	})
}

func TestRemoteLifecycle(t *testing.T) {
	forge, reg := remoteFixture(t)

	code, stdout, _ := cli(t, "list", "--registry", reg)
	if code != 0 || !strings.Contains(stdout, "acme — Acme (acme/templates@main)") ||
		!strings.Contains(stdout, "acme/demo") || !strings.Contains(stdout, "acme/bare") {
		t.Errorf("list: %d %q", code, stdout)
	}
	code, stdout, _ = cli(t, "list", "--registry", reg, "--kind", "cli", "--tags")
	if code != 0 || !strings.Contains(stdout, "kind: cli · lang: go") || strings.Contains(stdout, "acme/bare") {
		t.Errorf("list --kind cli --tags: %d %q", code, stdout)
	}
	code, stdout, _ = cli(t, "list", "--registry", reg, "--language", "cobol")
	if code != 0 || !strings.Contains(stdout, "no form matches that filter") {
		t.Errorf("list with no match: %d %q", code, stdout)
	}

	proj := filepath.Join(t.TempDir(), "proj")
	code, stdout, stderr := cli(t, "init", "acme/demo", "--registry", reg, "--out", proj, "--set", "project_name=Hello World")
	if code != 0 || !strings.Contains(stdout, "fetching github:acme/templates@main (forms/demo)") ||
		!strings.Contains(stdout, "initialized acme/demo -> "+proj) {
		t.Fatalf("init: %d %q %q", code, stdout, stderr)
	}
	ans := read(t, filepath.Join(proj, ".templetry-answers.yml"))
	if !strings.Contains(ans, `source: "github.com/acme/templates@main/forms/demo"`) || !strings.Contains(ans, "commit: 1111111aaaa") {
		t.Errorf("answers after init = %s", ans)
	}
	code, _, stderr = cli(t, "init", "acme/demo", "--registry", reg, "--out", proj, "--set", "project_name=Hello World")
	if code != 1 || !strings.Contains(stderr, "not empty") {
		t.Errorf("init into a non-empty dir: %d %q", code, stderr)
	}
	code, _, stderr = cli(t, "init", "acme/nope", "--registry", reg, "--out", t.TempDir())
	if code != 1 || !strings.Contains(stderr, `no form "nope"`) {
		t.Errorf("init of an unknown form: %d %q", code, stderr)
	}

	code, stdout, _ = cli(t, "update", proj)
	if code != 0 || !strings.Contains(stdout, "up to date") {
		t.Errorf("update right after init: %d %q", code, stdout)
	}

	// The user edits the README; the template changes it too, elsewhere.
	readme := filepath.Join(proj, "README.md")
	if err := os.WriteFile(readme, []byte("# hello-world\n\nMy notes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The README now differs on the user's side and the template's; the
	// base render tells them apart.
	publishDemo(t, forge, "2222222bbbb", "<!-- v2 -->\n# template-app\n")

	code, stdout, _ = cli(t, "update", proj)
	if code != 0 || !strings.Contains(stdout, "1111111 -> 2222222") || !strings.Contains(stdout, "merged    README.md") ||
		!strings.Contains(stdout, "preview only") {
		t.Fatalf("update preview: %d %q", code, stdout)
	}
	if got := read(t, readme); got != "# hello-world\n\nMy notes.\n" {
		t.Errorf("preview wrote to disk: %q", got)
	}
	// The directory comes first and --apply still counts.
	code, stdout, _ = cli(t, "update", proj, "--apply")
	if code != 0 || !strings.Contains(stdout, "update applied: 1 files written") {
		t.Fatalf("update --apply: %d %q", code, stdout)
	}
	if got := read(t, readme); got != "<!-- v2 -->\n# hello-world\n\nMy notes.\n" {
		t.Errorf("README after update = %q, want the template's line and the user's notes", got)
	}
	if ans := read(t, filepath.Join(proj, ".templetry-answers.yml")); !strings.Contains(ans, "commit: 2222222bbbb") {
		t.Errorf("anchor not moved: %s", ans)
	}

	// Pieces from the recorded remote source.
	code, stdout, _ = cli(t, "pieces", proj, "--registry", reg)
	if code != 0 || !strings.Contains(stdout, "  ci") {
		t.Errorf("pieces: %d %q", code, stdout)
	}
	code, stdout, stderr = cli(t, "add", "ci", proj, "--registry", reg)
	if code != 0 || !strings.Contains(stdout, "piece ci applied") {
		t.Fatalf("add: %d %q %q", code, stdout, stderr)
	}
	if ans := read(t, filepath.Join(proj, ".templetry-answers.yml")); !strings.Contains(ans, `source: "github.com/acme/templates@main/forms/demo/pieces/ci"`) {
		t.Errorf("piece source not recorded: %s", ans)
	}
	code, stdout, _ = cli(t, "pieces", proj, "--registry", reg)
	if code != 0 || !strings.Contains(stdout, "* ci") {
		t.Errorf("pieces after add must mark ci applied: %q", stdout)
	}
}
