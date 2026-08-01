// Command templetry is the CLI for the Templetry engine.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Templetry/engine/manifest"
	"github.com/Templetry/engine/ops"
	"github.com/Templetry/engine/planner"
	"github.com/Templetry/engine/render"
	"github.com/Templetry/engine/source"
)

// version is stamped by the release build via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "version":
		fmt.Println("templetry", version)
	case "plan":
		err = runPlan(os.Args[2:])
	case "render":
		err = runRender(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`templetry — project scaffolding for every platform, delivered to any forge

usage:
  templetry plan   --template <dir> [--set k=v]... [--feature k[=false]]... [--json]
  templetry render --template <dir> --out <dir> [--set k=v]... [--feature k[=false]]... [--force]
  templetry version

Spec: https://github.com/Templetry/wiki
`)
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func parseInputs(sets, feats []string) (manifest.Inputs, error) {
	in := manifest.Inputs{Variables: map[string]string{}, Features: map[string]bool{}}
	for _, s := range sets {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return in, fmt.Errorf("--set wants key=value, got %q", s)
		}
		in.Variables[k] = v
	}
	for _, f := range feats {
		k, v, has := strings.Cut(f, "=")
		if k == "" {
			return in, fmt.Errorf("--feature wants key or key=false, got %q", f)
		}
		switch {
		case !has || v == "true":
			in.Features[k] = true
		case v == "false":
			in.Features[k] = false
		default:
			return in, fmt.Errorf("--feature %s: value must be true or false", k)
		}
	}
	return in, nil
}

func buildPlan(templateDir string, in manifest.Inputs) (*ops.Plan, *source.FileSet, error) {
	name := filepath.Join(templateDir, "template.yml")
	data, err := os.ReadFile(name)
	if os.IsNotExist(err) {
		data, err = os.ReadFile(filepath.Join(templateDir, "template.yaml"))
	}
	if err != nil {
		return nil, nil, fmt.Errorf("no template.yml in %s: %w", templateDir, err)
	}
	m, err := manifest.Load(data)
	if err != nil {
		return nil, nil, err
	}
	files, err := source.LoadDir(templateDir)
	if err != nil {
		return nil, nil, err
	}
	p, err := planner.Build(m, in, files)
	if err != nil {
		return nil, nil, err
	}
	return p, files, nil
}

func runPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	template := fs.String("template", "", "template directory")
	asJSON := fs.Bool("json", false, "emit the plan as JSON")
	var sets, feats multiFlag
	fs.Var(&sets, "set", "variable value key=value (repeatable)")
	fs.Var(&feats, "feature", "feature toggle key or key=false (repeatable)")
	fs.Parse(args)
	if *template == "" {
		return fmt.Errorf("--template is required")
	}
	in, err := parseInputs(sets, feats)
	if err != nil {
		return err
	}
	p, _, err := buildPlan(*template, in)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}
	fmt.Print(p.Describe())
	return nil
}

func runRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	template := fs.String("template", "", "template directory")
	out := fs.String("out", "", "output directory")
	force := fs.Bool("force", false, "write into a non-empty output directory")
	var sets, feats multiFlag
	fs.Var(&sets, "set", "variable value key=value (repeatable)")
	fs.Var(&feats, "feature", "feature toggle key or key=false (repeatable)")
	fs.Parse(args)
	if *template == "" || *out == "" {
		return fmt.Errorf("--template and --out are required")
	}
	in, err := parseInputs(sets, feats)
	if err != nil {
		return err
	}
	p, files, err := buildPlan(*template, in)
	if err != nil {
		return err
	}
	if entries, err := os.ReadDir(*out); err == nil && len(entries) > 0 && !*force {
		return fmt.Errorf("output directory %s is not empty (use --force)", *out)
	}
	result, err := render.Apply(p, files)
	if err != nil {
		return err
	}
	if err := render.WriteDir(result, *out); err != nil {
		return err
	}
	fmt.Printf("rendered %s -> %s (%d files)\n", p.Template, *out, result.Len())
	return nil
}
