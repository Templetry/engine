// Package answers reads and writes .templetry-answers.yml — the provenance
// record every render carries (spec/answers-file.md). One deterministic
// emitter serves renders, updates and pieces alike.
package answers

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// Path is the answers file name at a project root.
const Path = ".templetry-answers.yml"

// Answers is the full parsed record.
type Answers struct {
	SchemaVersion int `yaml:"schema_version"`
	Template      struct {
		Name   string `yaml:"name"`
		Source string `yaml:"source"`
		Commit string `yaml:"commit"`
	} `yaml:"template"`
	Variables map[string]string `yaml:"variables"`
	Features  map[string]bool   `yaml:"features"`
	Pieces    []AppliedPiece    `yaml:"pieces"`
}

// AppliedPiece records one piece applied to the project (ADR-0014): its
// own drift anchor, sub-customization and owned files.
type AppliedPiece struct {
	Name      string            `yaml:"name"`
	Source    string            `yaml:"source"`
	Commit    string            `yaml:"commit"`
	Variables map[string]string `yaml:"variables"`
	Files     []string          `yaml:"files"`
}

// Read loads a project's answers file.
func Read(dir string) (Answers, error) {
	var a Answers
	data, err := os.ReadFile(filepath.Join(dir, Path))
	if err != nil {
		return a, fmt.Errorf("no answers file in %s", dir)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if err := yaml.Unmarshal(data, &a); err != nil {
		return a, err
	}
	return a, nil
}

var bareScalarRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 ._-]*$`)

func scalar(s string) string {
	if bareScalarRe.MatchString(s) && !strings.HasSuffix(s, " ") {
		return s
	}
	return strconv.Quote(s)
}

// Marshal emits the deterministic answers document: sorted keys, no
// timestamp, byte-stable for identical inputs (NFR4).
func Marshal(a Answers) []byte {
	var b strings.Builder
	b.WriteString("schema_version: 1\n")
	b.WriteString("template:\n")
	fmt.Fprintf(&b, "  name: %s\n", a.Template.Name)
	src := a.Template.Source
	if src == "" {
		src = "local"
	}
	fmt.Fprintf(&b, "  source: %s\n", scalar(src))
	if a.Template.Commit != "" {
		fmt.Fprintf(&b, "  commit: %s\n", a.Template.Commit)
	}
	if len(a.Variables) > 0 {
		b.WriteString("variables:\n")
		for _, k := range sortedKeys(a.Variables) {
			fmt.Fprintf(&b, "  %s: %s\n", k, scalar(a.Variables[k]))
		}
	}
	if len(a.Features) > 0 {
		b.WriteString("features:\n")
		keys := make([]string, 0, len(a.Features))
		for k := range a.Features {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: %t\n", k, a.Features[k])
		}
	}
	if len(a.Pieces) > 0 {
		b.WriteString("pieces:\n")
		for _, p := range a.Pieces {
			fmt.Fprintf(&b, "  - name: %s\n", p.Name)
			fmt.Fprintf(&b, "    source: %s\n", scalar(p.Source))
			if p.Commit != "" {
				fmt.Fprintf(&b, "    commit: %s\n", p.Commit)
			}
			if len(p.Variables) > 0 {
				b.WriteString("    variables:\n")
				for _, k := range sortedKeys(p.Variables) {
					fmt.Fprintf(&b, "      %s: %s\n", k, scalar(p.Variables[k]))
				}
			}
			if len(p.Files) > 0 {
				b.WriteString("    files:\n")
				files := append([]string(nil), p.Files...)
				sort.Strings(files)
				for _, f := range files {
					fmt.Fprintf(&b, "      - %s\n", scalar(f))
				}
			}
		}
	}
	return []byte(b.String())
}

// Write persists the record at the project root.
func Write(dir string, a Answers) error {
	return os.WriteFile(filepath.Join(dir, Path), Marshal(a), 0o644)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
