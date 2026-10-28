package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Templetry/engine/catalog"
	"github.com/Templetry/engine/manifest"
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

func runList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	registry := fs.String("registry", "", "registry URL or file (default: official catalog)")
	tags := fs.Bool("tags", false, "show each form's kinds, languages and frameworks")
	var kinds, languages, frameworks multiFlag
	fs.Var(&kinds, "kind", "filter by kind (repeatable): "+strings.Join(manifest.Kinds, ", "))
	fs.Var(&languages, "language", "filter by language (repeatable)")
	fs.Var(&frameworks, "framework", "filter by framework (repeatable)")
	if err := parseFlags(fs, args, stderr); err != nil {
		return err
	}

	for _, k := range kinds {
		if !manifest.ValidKind(k) {
			return fmt.Errorf("unknown kind %q (allowed: %s)", k, strings.Join(manifest.Kinds, ", "))
		}
	}
	filter := catalog.Filter{Kinds: kinds, Languages: languages, Frameworks: frameworks}

	reg, err := loadRegistry(*registry)
	if err != nil {
		return err
	}
	matched := 0
	for _, p := range reg.Parents {
		forms := []catalog.Form{}
		for _, f := range p.Forms {
			if filter.Match(f) {
				forms = append(forms, f)
			}
		}
		if len(forms) == 0 {
			continue
		}
		fmt.Fprintf(stdout, "%s — %s (%s@%s)\n", p.Key, p.Label, p.Repo, p.Ref)
		for _, f := range forms {
			matched++
			status := f.Status
			if status == "" {
				status = "ready"
			}
			fmt.Fprintf(stdout, "  %-28s %-8s %s\n", p.Key+"/"+f.Form, status, f.Description)
			if *tags {
				fmt.Fprintf(stdout, "  %-28s %-8s %s\n", "", "", formatTags(f))
			}
		}
	}
	if matched == 0 && !filter.Empty() {
		// Say so rather than printing nothing: a form that declares no
		// taxonomy matches no filter, and silence looks like a broken catalog.
		fmt.Fprintln(stdout, "no form matches that filter (forms without a taxonomy never match one)")
	}
	return nil
}

// formatTags renders a form's taxonomy on one line, omitting empty axes.
func formatTags(f catalog.Form) string {
	parts := []string{}
	for _, axis := range []struct {
		label  string
		values []string
	}{{"kind", f.Kinds}, {"lang", f.Languages}, {"fw", f.Frameworks}} {
		if len(axis.values) > 0 {
			parts = append(parts, axis.label+": "+strings.Join(axis.values, " "))
		}
	}
	if len(parts) == 0 {
		return "(no taxonomy)"
	}
	return strings.Join(parts, " · ")
}

func runInit(args []string, stdout, stderr io.Writer) error {
	ref := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		ref = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	out := fs.String("out", "", "output directory")
	registry := fs.String("registry", "", "registry URL or file (default: official catalog)")
	force := fs.Bool("force", false, "write into a non-empty output directory")
	preset := fs.String("preset", "", "named feature combo from the manifest")
	var sets, feats multiFlag
	fs.Var(&sets, "set", "variable value key=value (repeatable)")
	fs.Var(&feats, "feature", "feature toggle key or key=false (repeatable)")
	if err := parseFlags(fs, args, stderr); err != nil {
		return err
	}
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
	src, err := source.ParseRef(parent.SourceRef())
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "fetching %s@%s (%s)…\n", src, parent.Ref, form.Path)
	files, err := source.Fetch(src, parent.Ref, form.Path, os.Getenv("TEMPLETRY_TOKEN"))
	if err != nil {
		return err
	}
	p, err := planFromFiles(files, in)
	if err != nil {
		return err
	}
	p.Source = source.FormatSource(src, parent.Ref, form.Path)
	if sha, err := source.ResolveRef(src, parent.Ref, os.Getenv("TEMPLETRY_TOKEN")); err == nil {
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
	fmt.Fprintf(stdout, "initialized %s -> %s (%d files)\n", ref, *out, result.Len())
	return nil
}
