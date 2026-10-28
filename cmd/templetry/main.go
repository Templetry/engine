// Command templetry is the CLI for the Templetry engine.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
	if code := run(os.Args[1:], os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

// run dispatches one command line (without the program name) and returns
// the process exit code: 0 on success, 1 when the command fails, 2 for a
// usage error.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		usage(stdout)
		return 2
	}
	var err error
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, "templetry", version)
	case "plan":
		err = runPlan(args[1:], stdout, stderr)
	case "render":
		err = runRender(args[1:], stdout, stderr)
	case "verify":
		err = runVerify(args[1:], stdout, stderr)
	case "update":
		err = runUpdate(args[1:], stdout, stderr)
	case "pieces":
		err = runPieces(args[1:], stdout, stderr)
	case "add":
		err = runAdd(args[1:], stdout, stderr)
	case "init":
		err = runInit(args[1:], stdout, stderr)
	case "list":
		err = runList(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		usage(stdout)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		usage(stdout)
		return 2
	}
	var code flagExit
	if errors.As(err, &code) {
		return int(code)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// flagExit carries the exit status flag.ExitOnError would have used: the
// flag package has already printed the problem (or the help) to stderr.
type flagExit int

func (e flagExit) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// parseFlags parses a subcommand's flags with flag.ExitOnError's
// observable behaviour — messages on stderr, status 0 for -h and 2 for a
// bad flag — but returns instead of exiting, so run stays testable.
func parseFlags(fs *flag.FlagSet, args []string, stderr io.Writer) error {
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return flagExit(0)
		}
		return flagExit(2)
	}
	return nil
}

func usage(stdout io.Writer) {
	fmt.Fprint(stdout, `templetry - project scaffolding for every platform, delivered to any forge

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

func runPlan(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	template := fs.String("template", "", "template directory")
	asJSON := fs.Bool("json", false, "emit the plan as JSON")
	preset := fs.String("preset", "", "named feature combo from the manifest")
	var sets, feats multiFlag
	fs.Var(&sets, "set", "variable value key=value (repeatable)")
	fs.Var(&feats, "feature", "feature toggle key or key=false (repeatable)")
	if err := parseFlags(fs, args, stderr); err != nil {
		return err
	}
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
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}
	fmt.Fprint(stdout, p.Describe())
	return nil
}

func runRender(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	template := fs.String("template", "", "template directory")
	out := fs.String("out", "", "output directory")
	force := fs.Bool("force", false, "write into a non-empty output directory")
	preset := fs.String("preset", "", "named feature combo from the manifest")
	var sets, feats multiFlag
	fs.Var(&sets, "set", "variable value key=value (repeatable)")
	fs.Var(&feats, "feature", "feature toggle key or key=false (repeatable)")
	if err := parseFlags(fs, args, stderr); err != nil {
		return err
	}
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
	fmt.Fprintf(stdout, "rendered %s -> %s (%d files)\n", p.Template, *out, result.Len())
	return nil
}

// runVerify renders the template (or takes an existing render via --dir)
// and executes its manifest's verify command in Docker (ADR-0004).
func runVerify(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	template := fs.String("template", "", "template directory")
	dir := fs.String("dir", "", "already-rendered directory to verify (default: render to a temp dir)")
	keep := fs.Bool("keep", false, "keep the temporary render and print its path")
	preset := fs.String("preset", "", "named feature combo from the manifest")
	var sets, feats multiFlag
	fs.Var(&sets, "set", "variable value key=value (repeatable)")
	fs.Var(&feats, "feature", "feature toggle key or key=false (repeatable)")
	if err := parseFlags(fs, args, stderr); err != nil {
		return err
	}
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
			fmt.Fprintf(stdout, "render kept at %s\n", tmp)
		} else {
			defer os.RemoveAll(tmp)
		}
		if err := render.WriteDir(result, tmp); err != nil {
			return err
		}
		target = tmp
	}

	fmt.Fprintf(stdout, "verify: %s in %s\n", m.Verify.Run, m.Verify.Image)
	if err := verify.Run(m.Verify.Image, m.Verify.Run, target, stdout, stderr); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "verify: OK")
	return nil
}

// runUpdate previews (default) or applies the template update cycle on an
// existing project: re-render at the template's head with the recorded
// inputs, diff, three-way merge, write.
func runUpdate(args []string, stdout, stderr io.Writer) error {
	// The directory is positional and may come first; Go's flag package
	// stops at the first non-flag argument, so pull it out before parsing
	// or "update <dir> --apply" would silently preview instead of writing.
	dir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "write the update (default: preview only)")
	token := fs.String("token", "", "GitHub token for private templates / rate limits")
	if err := parseFlags(fs, args, stderr); err != nil {
		return err
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
	fmt.Fprintf(stdout, "template %s · %s -> %s\n", p.Template, short(p.OldCommit), short(p.NewCommit))
	if len(p.Entries) == 0 {
		fmt.Fprintf(stdout, "up to date (%d files unchanged)\n", p.Unchanged)
		return nil
	}
	for _, e := range p.Entries {
		owner := ""
		if e.Piece != "" {
			owner = "  [" + e.Piece + "]"
		}
		fmt.Fprintf(stdout, "  %-9s %s%s\n", e.Status, e.Path, owner)
	}
	fmt.Fprintf(stdout, "  %d unchanged\n", p.Unchanged)
	if !*apply {
		fmt.Fprintln(stdout, "preview only — run again with --apply to write")
		return nil
	}
	n, err := p.Apply()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "update applied: %d files written — review with git before committing\n", n)
	return nil
}
