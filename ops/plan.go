package ops

import (
	"fmt"
	"strings"

	"github.com/Templetry/engine/manifest"
)

// Action says what happens to one template file.
type Action string

const (
	Copy    Action = "copy"    // byte-for-byte copy (binaries), path may still be renamed
	Render  Action = "render"  // text pass: directives, identity replacements, patches
	Exclude Action = "exclude" // dropped: inactive feature or the manifest itself
)

// Replacement is one identity substitution, applied to file content.
type Replacement struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// FileAction is the planned fate of a single template file.
type FileAction struct {
	Src     string           `json:"src"`
	Dest    string           `json:"dest,omitempty"`
	Action  Action           `json:"action"`
	Patches []manifest.Patch `json:"patches,omitempty"`
	Reason  string           `json:"reason,omitempty"` // for excludes
}

// Plan is the complete, serializable description of one render. It is pure
// data: printing it is the dry-run, executing it is the render (ADR study II).
type Plan struct {
	Template     string            `json:"template"`
	Source       string            `json:"source,omitempty"` // template origin; empty means local
	Variables    map[string]string `json:"variables"`
	Features     map[string]bool   `json:"features"`
	Replacements []Replacement     `json:"replacements,omitempty"`
	Files        []FileAction      `json:"files"`
}

// Describe renders the plan as human-readable dry-run output.
func (p *Plan) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "template %s\n", p.Template)
	var rendered, copied, excluded, patched int
	for _, f := range p.Files {
		switch f.Action {
		case Render:
			rendered++
			if f.Dest != f.Src {
				fmt.Fprintf(&b, "  render  %s -> %s\n", f.Src, f.Dest)
			} else {
				fmt.Fprintf(&b, "  render  %s\n", f.Src)
			}
			if len(f.Patches) > 0 {
				patched++
				fmt.Fprintf(&b, "  patch   %s (%d ops)\n", f.Dest, len(f.Patches))
			}
		case Copy:
			copied++
			if f.Dest != f.Src {
				fmt.Fprintf(&b, "  copy    %s -> %s\n", f.Src, f.Dest)
			} else {
				fmt.Fprintf(&b, "  copy    %s\n", f.Src)
			}
		case Exclude:
			excluded++
			fmt.Fprintf(&b, "  exclude %s (%s)\n", f.Src, f.Reason)
		}
	}
	fmt.Fprintf(&b, "%d rendered, %d copied, %d excluded, %d patched, + .templetry-answers.yml\n",
		rendered, copied, excluded, patched)
	return b.String()
}
