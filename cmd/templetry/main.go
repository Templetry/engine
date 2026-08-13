// Command templetry is the CLI for the Templetry engine.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Templetry/engine/manifest"
	"github.com/Templetry/engine/ops"
	"github.com/Templetry/engine/planner"
	"github.com/Templetry/engine/render"
	"github.com/Templetry/engine/source"
	"github.com/Templetry/engine/update"
	"github.com/Templetry/engine/verify"
)

// version is stamped by the release build via -ldflags "-X main.version=...".
var version = "1.0.0-dev"

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
	case "verify":
		err = runVerify(os.Args[2:])
	case "update":
		err = runUpdate(os.Args[2:])
	case "pieces":
		err = runPieces(os.Args[2:])
	case "add":
		err = runAdd(os.Args[2:])
	case "init":
		err = runInit(os.Args[2:])
	case "list":
		err = runList(os.Args[2:])
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
	fmt.Print(`templetry - project scaffolding for every platform, delivered to any forge

usage:
  templetry list   [--registry <url|file>]
  templetry init   <parent>/<form> --out <dir> [--set k=v]... [--feature k[=false]]... [--preset k] [--force]
  templetry plan   --template <dir> [--set k=v]... [--feature k[=false]]... [--preset k] [--json]
  templetry render --template <dir> --out <dir> [--set k=v]... [--feature k[=false]]... [--preset k] [--force]
  templetry verify --template <dir> [--dir <rendered>] [--set k=v]... [--feature k[=false]]... [--preset k] [--keep]
  templetry update [dir] [--apply] [--token <github-token>]
  templetry pieces [dir] [--template <dir>]
  templetry add    <piece> [dir] [--set k=v]... [--template <dir>]
  templetry version

Catalog: https://github.com/Templetry/catalog | Spec: https://github.com/Templetry/wiki
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

func loadManifest(files *source.FileSet) (*manifest.Manifest, error) {
	f := files.Get("template.yml")
	if f == nil {
		f = files.Get("template.yaml")
	}
	if f == nil {
		return nil, fmt.Errorf("the template has no template.yml")
	}
	return manifest.Load(f.Data)
}

// planFromFiles builds a plan from an in-memory template (local dir or
// remote tarball alike).
func planFromFiles(files *source.FileSet, in manifest.Inputs) (*ops.Plan, error) {
	m, err := loadManifest(files)
	if err != nil {
		return nil, err
	}
	return planner.Build(m, in, files)
}

func buildPlan(templateDir string, in manifest.Inputs) (*ops.Plan, *source.FileSet, error) {
	files, err := source.LoadDir(templateDir)
	if err != nil {
		return nil, nil, err
	}
	p, err := planFromFiles(files, in)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", templateDir, err)
	}
	return p, files, nil
}

func runPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	template := fs.String("template", "", "template directory")
	asJSON := fs.Bool("json", false, "emit the plan as JSON")
	preset := fs.String("preset", "", "named feature combo from the manifest")
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
	in.Preset = *preset
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
	preset := fs.String("preset", "", "named feature combo from the manifest")
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
	in.Preset = *preset
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

// runVerify renders the template (or takes an existing render via --dir)
// and executes its manifest's verify command in Docker (ADR-0004).
func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	template := fs.String("template", "", "template directory")
	dir := fs.String("dir", "", "already-rendered directory to verify (default: render to a temp dir)")
	keep := fs.Bool("keep", false, "keep the temporary render and print its path")
	preset := fs.String("preset", "", "named feature combo from the manifest")
	var sets, feats multiFlag
	fs.Var(&sets, "set", "variable value key=value (repeatable)")
	fs.Var(&feats, "feature", "feature toggle key or key=false (repeatable)")
	fs.Parse(args)
	if *template == "" {
		return fmt.Errorf("--template is required")
	}
	files, err := source.LoadDir(*template)
	if err != nil {
		return err
	}
	m, err := loadManifest(files)
	if err != nil {
		return fmt.Errorf("%s: %w", *template, err)
	}
	if m.Verify == nil {
		return fmt.Errorf("template %q declares no verify — add verify: {image, run} to its template.yml", m.Name)
	}

	target := *dir
	if target == "" {
		in, err := parseInputs(sets, feats)
		if err != nil {
			return err
		}
		in.Preset = *preset
		p, err := planner.Build(m, in, files)
		if err != nil {
			return err
		}
		result, err := render.Apply(p, files)
		if err != nil {
			return err
		}
		tmp, err := os.MkdirTemp("", "templetry-verify-")
		if err != nil {
			return err
		}
		if *keep {
			fmt.Printf("render kept at %s\n", tmp)
		} else {
			defer os.RemoveAll(tmp)
		}
		if err := render.WriteDir(result, tmp); err != nil {
			return err
		}
		target = tmp
	}

	fmt.Printf("verify: %s in %s\n", m.Verify.Run, m.Verify.Image)
	if err := verify.Run(m.Verify.Image, m.Verify.Run, target, os.Stdout, os.Stderr); err != nil {
		return err
	}
	fmt.Println("verify: OK")
	return nil
}

// runUpdate previews (default) or applies the template update cycle on an
// existing project: re-render at the template's head with the recorded
// inputs, diff, three-way merge, write.
func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	apply := fs.Bool("apply", false, "write the update (default: preview only)")
	token := fs.String("token", "", "GitHub token for private templates / rate limits")
	fs.Parse(args)
	dir := fs.Arg(0)
	if dir == "" {
		dir = "."
	}

	p, err := update.Prepare(dir, *token)
	if err != nil {
		return err
	}
	short := func(s string) string {
		if len(s) > 7 {
			return s[:7]
		}
		return s
	}
	fmt.Printf("template %s · %s -> %s\n", p.Template, short(p.OldCommit), short(p.NewCommit))
	if len(p.Entries) == 0 {
		fmt.Printf("up to date (%d files unchanged)\n", p.Unchanged)
		return nil
	}
	for _, e := range p.Entries {
		fmt.Printf("  %-9s %s\n", e.Status, e.Path)
	}
	fmt.Printf("  %d unchanged\n", p.Unchanged)
	if !*apply {
		fmt.Println("preview only — run again with --apply to write")
		return nil
	}
	n, err := p.Apply()
	if err != nil {
		return err
	}
	fmt.Printf("update applied: %d files written — review with git before committing\n", n)
	return nil
}
