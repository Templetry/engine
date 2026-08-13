package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/Templetry/engine/answers"
	"github.com/Templetry/engine/piece"
	"github.com/Templetry/engine/source"
)

// formFilesFor loads the template a project was rendered from: its
// recorded GitHub source, or an explicit --template dir for local flows.
// Returns the files and, for GitHub sources, the resolved head commit.
func formFilesFor(ans answers.Answers, templateDir string) (*source.FileSet, string, error) {
	if templateDir != "" {
		files, err := source.LoadDir(templateDir)
		return files, "", err
	}
	return piece.FetchForm(ans)
}

// runPieces lists the pieces the project's template ships.
func runPieces(args []string) error {
	dir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("pieces", flag.ExitOnError)
	template := fs.String("template", "", "template directory (for local templates)")
	fs.Parse(args)
	ans, err := answers.Read(dir)
	if err != nil {
		return err
	}
	files, _, err := formFilesFor(ans, *template)
	if err != nil {
		return err
	}
	infos := piece.List(files, ans)
	if len(infos) == 0 {
		fmt.Println("this template ships no pieces")
		return nil
	}
	for _, in := range infos {
		mark := " "
		if in.Applied {
			mark = "*"
		}
		fmt.Printf("%s %-24s %s\n", mark, in.Name, in.Description)
	}
	fmt.Println("(* already applied — add with: templetry add <piece>)")
	return nil
}

// runAdd applies one piece to an existing project (ADR-0014).
func runAdd(args []string) error {
	name := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		args = args[1:]
	}
	dir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	template := fs.String("template", "", "template directory (for local templates)")
	var sets multiFlag
	fs.Var(&sets, "set", "piece variable key=value (repeatable)")
	fs.Parse(args)
	if name == "" {
		return fmt.Errorf("usage: templetry add <piece> [dir] (try 'templetry pieces')")
	}

	ans, err := answers.Read(dir)
	if err != nil {
		return err
	}
	for _, p := range ans.Pieces {
		if p.Name == name {
			return fmt.Errorf("piece %q is already applied (added %s)", name, p.Commit)
		}
	}
	files, commit, err := formFilesFor(ans, *template)
	if err != nil {
		return err
	}
	formM, err := loadManifest(files)
	if err != nil {
		return err
	}
	pieceFiles, err := piece.Extract(files, name)
	if err != nil {
		return err
	}
	pf := pieceFiles.Get("piece.yml")
	if pf == nil {
		pf = pieceFiles.Get("piece.yaml")
	}
	pm, err := piece.Load(pf.Data)
	if err != nil {
		return err
	}
	pieceVars := map[string]string{}
	for _, s := range sets {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return fmt.Errorf("--set wants key=value, got %q", s)
		}
		pieceVars[k] = v
	}

	res, err := piece.Apply(dir, formM, pm, pieceFiles, ans.Variables, pieceVars)
	if err != nil {
		return err
	}

	src := ans.Template.Source
	if src != "" && src != "local" {
		src += "/pieces/" + name
	}
	ans.Pieces = append(ans.Pieces, answers.AppliedPiece{
		Name: name, Source: src, Commit: commit,
		Variables: res.Variables, Files: res.Files,
	})
	if err := answers.Write(dir, ans); err != nil {
		return err
	}
	fmt.Printf("piece %s applied: %d files", name, len(res.Files))
	if len(pm.Patches) > 0 {
		fmt.Printf(" + %d patches", len(pm.Patches))
	}
	fmt.Println(" — review with git before committing")
	return nil
}
