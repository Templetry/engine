package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Templetry/engine/catalog"
	"github.com/Templetry/engine/render"
	"github.com/Templetry/engine/source"
)

// loadRegistry reads a registry from a local file or URL; empty means the
// official Templetry catalog.
func loadRegistry(location string) (*catalog.Registry, error) {
	if location == "" {
		location = catalog.DefaultRegistryURL
	}
	if data, err := os.ReadFile(location); err == nil {
		return catalog.Parse(data)
	}
	resp, err := http.Get(location)
	if err != nil {
		return nil, fmt.Errorf("fetching registry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching registry: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return catalog.Parse(data)
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	registry := fs.String("registry", "", "registry URL or file (default: official catalog)")
	fs.Parse(args)
	reg, err := loadRegistry(*registry)
	if err != nil {
		return err
	}
	for _, p := range reg.Parents {
		fmt.Printf("%s — %s (%s@%s)\n", p.Key, p.Label, p.Repo, p.Ref)
		for _, f := range p.Forms {
			status := f.Status
			if status == "" {
				status = "ready"
			}
			fmt.Printf("  %-28s %-8s %s\n", p.Key+"/"+f.Form, status, f.Description)
		}
	}
	return nil
}

func runInit(args []string) error {
	ref := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		ref = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	out := fs.String("out", "", "output directory")
	registry := fs.String("registry", "", "registry URL or file (default: official catalog)")
	force := fs.Bool("force", false, "write into a non-empty output directory")
	preset := fs.String("preset", "", "named feature combo from the manifest")
	var sets, feats multiFlag
	fs.Var(&sets, "set", "variable value key=value (repeatable)")
	fs.Var(&feats, "feature", "feature toggle key or key=false (repeatable)")
	fs.Parse(args)
	if ref == "" {
		return fmt.Errorf("usage: templetry init <parent>/<form> --out <dir> (try 'templetry list')")
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	in, err := parseInputs(sets, feats)
	if err != nil {
		return err
	}
	in.Preset = *preset
	reg, err := loadRegistry(*registry)
	if err != nil {
		return err
	}
	parent, form, err := reg.Resolve(ref)
	if err != nil {
		return err
	}
	fmt.Printf("fetching %s@%s (%s)…\n", parent.Repo, parent.Ref, form.Path)
	files, err := source.FetchGitHubTarball(parent.Repo, parent.Ref, form.Path)
	if err != nil {
		return err
	}
	p, err := planFromFiles(files, in)
	if err != nil {
		return err
	}
	p.Source = fmt.Sprintf("github.com/%s@%s/%s", parent.Repo, parent.Ref, form.Path)
	if sha, err := source.ResolveGitHubRef(parent.Repo, parent.Ref, ""); err == nil {
		p.SourceCommit = sha
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
	fmt.Printf("initialized %s -> %s (%d files)\n", ref, *out, result.Len())
	return nil
}
