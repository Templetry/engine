package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Templetry/engine/internal/forgetest"
)

// TestRecordDemo records the README demo (docs/demo.svg) as an asciicast:
// every templetry line in it is the real CLI (run, the function main
// calls) against a fake forge, so the template can move from v1 to v2
// mid-recording. The shell lines (find, echo, cat) are performed in Go
// and print what those commands would.
//
// Skipped unless TEMPLETRY_DEMO_CAST names the output file. Regenerate:
//
//	TEMPLETRY_DEMO_CAST=$PWD/docs/demo.cast go test ./cmd/templetry -run TestRecordDemo
//	npx svg-term-cli@2.1.1 --in docs/demo.cast --out docs/demo.svg --window --no-cursor
func TestRecordDemo(t *testing.T) {
	castPath := os.Getenv("TEMPLETRY_DEMO_CAST")
	if castPath == "" {
		t.Skip("set TEMPLETRY_DEMO_CAST to record the README demo")
	}

	forge := forgetest.Install(t)
	v1 := map[string]string{
		"template.yml": `schema_version: 1
name: go-cli
variables:
  - key: project_name
    type: string
identity:
  - from: "template-app"
    to: "{project_name.kebab}"
`,
		"README.md":                "# template-app\n\nA command-line tool.\n\n## Usage\n\n    template-app --help\n",
		"go.mod":                   "module example.com/template-app\n\ngo 1.26\n",
		"main.go":                  "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"template-app\")\n}\n",
		"main_test.go":             "package main\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {}\n",
		".github/workflows/ci.yml": "name: CI\non: [push]\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - run: go test ./...\n",
	}
	v2 := map[string]string{}
	for k, v := range v1 {
		v2[k] = v
	}
	v2["README.md"] = strings.Replace(v1["README.md"], "A command-line tool.\n", "A command-line tool. Build it with `make build`.\n", 1)
	v2[".github/workflows/ci.yml"] = strings.Replace(v1[".github/workflows/ci.yml"], "      - run: go test ./...\n", "      - uses: actions/setup-go@v5\n      - run: go test -race ./...\n", 1)
	v2["Makefile"] = "build:\n\tgo build -o bin/template-app .\n"
	publish := func(commit string, files map[string]string) {
		out := map[string]string{}
		for p, d := range files {
			out["go/cli/"+p] = d
		}
		forge.Publish(t, "acme/templates", "main", commit, out)
	}
	publish("3f9c2a1e5b7d", v1)

	work := t.TempDir()
	t.Chdir(work)
	reg := `{"schema_version": 2, "parents": [{"key": "go", "label": "Go", "repo": "acme/templates", "ref": "main",
	  "forms": [{"form": "cli", "name": "cli", "path": "go/cli", "status": "ready", "description": "Go command-line tool with tests and CI"}]}]}`
	if err := os.WriteFile(filepath.Join(work, "registry.json"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &cast{}
	templetry := func(args ...string) {
		c.typeLine("templetry " + strings.Join(quoteAll(args), " "))
		var out bytes.Buffer
		// The registry flag points at the local fixture; the demo omits it
		// for readability, as the official catalog is the default.
		full := args
		if args[0] == "list" || args[0] == "init" {
			full = append(append([]string{}, args...), "--registry", "registry.json")
		}
		if code := run(full, &out, &out); code != 0 {
			t.Fatalf("templetry %v: exit %d\n%s", args, code, out.String())
		}
		c.print(out.String())
	}

	c.comment("# scaffold a project from the catalog")
	templetry("list")
	templetry("init", "go/cli", "--out", "my-tool", "--set", "project_name=My Tool")
	c.typeLine("find my-tool -type f | sort")
	c.print(findFiles(t, "my-tool"))

	c.comment("# make it yours")
	c.typeLine(`echo "Ships with love." >> my-tool/README.md`)
	appendFile(t, "my-tool/README.md", "Ships with love.\n")

	c.comment("# later: the template improves upstream...")
	publish("8d41b06c92fa", v2)
	templetry("update", "my-tool")
	templetry("update", "my-tool", "--apply")
	c.typeLine("cat my-tool/README.md")
	data, err := os.ReadFile("my-tool/README.md")
	if err != nil {
		t.Fatal(err)
	}
	c.print(string(data))
	c.comment("# template changes in, your edit kept")
	c.pause(2.5)

	if err := os.WriteFile(castPath, c.bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// cast accumulates asciicast v2 events.
type cast struct {
	now    float64
	events [][3]any
}

func (c *cast) emit(s string) {
	c.events = append(c.events, [3]any{round(c.now), "o", s})
}

func round(f float64) float64 { return float64(int(f*1000)) / 1000 }

func (c *cast) pause(sec float64) { c.now += sec }

func (c *cast) prompt() { c.emit("\x1b[1;32m$\x1b[0m ") }

func (c *cast) typeLine(cmd string) {
	c.prompt()
	c.pause(0.4)
	for _, r := range cmd {
		c.emit(string(r))
		c.pause(0.035)
	}
	c.pause(0.3)
	c.emit("\r\n")
	c.pause(0.2)
}

func (c *cast) comment(s string) {
	c.prompt()
	c.emit("\x1b[2m" + s + "\x1b[0m\r\n")
	c.pause(0.9)
}

func (c *cast) print(out string) {
	for _, line := range strings.SplitAfter(out, "\n") {
		if line == "" {
			continue
		}
		c.emit(strings.ReplaceAll(line, "\n", "\r\n"))
		c.pause(0.06)
	}
	c.pause(1.4)
}

func (c *cast) bytes() []byte {
	var b bytes.Buffer
	header, _ := json.Marshal(map[string]any{
		"version": 2, "width": 84, "height": 30,
		"env": map[string]string{"SHELL": "/bin/bash", "TERM": "xterm-256color"},
	})
	b.Write(header)
	b.WriteByte('\n')
	for _, e := range c.events {
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if k, v, ok := strings.Cut(a, "="); ok && strings.Contains(v, " ") {
			a = fmt.Sprintf("%s=%q", k, v)
		}
		out[i] = a
	}
	return out
}

func findFiles(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			paths = append(paths, filepath.ToSlash(p))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return strings.Join(paths, "\n") + "\n"
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
