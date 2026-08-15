// Package catalog reads the Templetry template registry (registry.json,
// schema v2): parents grouping structural forms, each form a subdirectory
// of the parent repo (ADR-0011).
package catalog

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultRegistryURL is the official Templetry catalog.
const DefaultRegistryURL = "https://raw.githubusercontent.com/Templetry/catalog/main/registry.json"

type Registry struct {
	SchemaVersion int      `json:"schema_version"`
	Parents       []Parent `json:"parents"`
	// Pieces are common pieces living outside any form (ADR-0016):
	// additive in registry v2.1, so older registries keep validating.
	Pieces []CommonPiece `json:"pieces,omitempty"`
}

// CommonPiece points at a piece directory in a shared repository.
type CommonPiece struct {
	Name   string `json:"name"`             // the piece name projects ask for
	Repo   string `json:"repo"`             // owner/name on GitHub
	Ref    string `json:"ref"`              // branch or tag
	Path   string `json:"path"`             // directory holding piece.yml
	Source string `json:"source,omitempty"` // forge scheme; empty means GitHub with Repo
}

// SourceRef resolves the piece's forge reference.
func (c CommonPiece) SourceRef() string {
	if c.Source != "" {
		return c.Source
	}
	return c.Repo
}

type Parent struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	Repo  string `json:"repo"`
	Ref   string `json:"ref"`
	// Source is the optional forge scheme (registry v2.1, ADR-0015):
	// "gitlab:gitlab.com/group/proj" or "gitea:codeberg.org/owner/repo".
	// Empty means GitHub with Repo as owner/name — the historical shape.
	Source string `json:"source,omitempty"`
	Forms  []Form `json:"forms"`
}

// SourceRef resolves the parent's forge reference: the explicit Source
// when present, otherwise GitHub with Repo.
func (p Parent) SourceRef() string {
	if p.Source != "" {
		return p.Source
	}
	return p.Repo
}

type Form struct {
	Form        string `json:"form"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Status      string `json:"status"` // ready | planned
	Description string `json:"description,omitempty"`
	// The taxonomy, copied from the form's manifest (registry v2.2,
	// ADR-0017). It lives here because filtering reads the registry, which
	// is fetched anyway — reading 22 manifests would turn a listing into 22
	// downloads. Additive, so older registries keep validating.
	Kinds      []string `json:"kinds,omitempty"`
	Languages  []string `json:"languages,omitempty"`
	Frameworks []string `json:"frameworks,omitempty"`
}

// Filter selects forms by taxonomy: OR within an axis, AND across axes, so
// "--kind backend --kind cli --language go" means (backend or cli) and go.
// An empty axis does not constrain.
type Filter struct {
	Kinds      []string
	Languages  []string
	Frameworks []string
}

// Empty reports whether the filter would select everything.
func (f Filter) Empty() bool {
	return len(f.Kinds) == 0 && len(f.Languages) == 0 && len(f.Frameworks) == 0
}

// Match reports whether a form satisfies the filter.
func (f Filter) Match(form Form) bool {
	return anyOf(f.Kinds, form.Kinds) &&
		anyOf(f.Languages, form.Languages) &&
		anyOf(f.Frameworks, form.Frameworks)
}

// anyOf is true when the form carries at least one of the wanted values, or
// when nothing is wanted. A form with no taxonomy matches no filter, which
// is honest: it never claimed to be anything.
func anyOf(wanted, have []string) bool {
	if len(wanted) == 0 {
		return true
	}
	for _, w := range wanted {
		for _, h := range have {
			if strings.EqualFold(w, h) {
				return true
			}
		}
	}
	return false
}

// Parse decodes and validates a registry document.
func Parse(data []byte) (*Registry, error) {
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	if r.SchemaVersion != 2 {
		return nil, fmt.Errorf("registry: schema_version must be 2, got %d", r.SchemaVersion)
	}
	for _, p := range r.Parents {
		if p.Key == "" || p.Repo == "" || p.Ref == "" {
			return nil, fmt.Errorf("registry: parent %q needs key, repo and ref", p.Key)
		}
		for _, f := range p.Forms {
			if f.Form == "" || f.Path == "" {
				return nil, fmt.Errorf("registry: parent %q has a form without form/path", p.Key)
			}
		}
	}
	return &r, nil
}

// Resolve finds a form by "<parent>/<form>" reference. Planned forms resolve
// with an error so callers fail with a clear message.
func (r *Registry) Resolve(ref string) (*Parent, *Form, error) {
	parentKey, formKey, ok := strings.Cut(ref, "/")
	if !ok || parentKey == "" || formKey == "" {
		return nil, nil, fmt.Errorf("template reference must be <parent>/<form>, got %q", ref)
	}
	for i := range r.Parents {
		p := &r.Parents[i]
		if p.Key != parentKey {
			continue
		}
		for j := range p.Forms {
			f := &p.Forms[j]
			if f.Form != formKey {
				continue
			}
			if f.Status != "" && f.Status != "ready" {
				return nil, nil, fmt.Errorf("form %s/%s is %s, not ready yet", parentKey, formKey, f.Status)
			}
			return p, f, nil
		}
		available := make([]string, 0, len(p.Forms))
		for _, f := range p.Forms {
			available = append(available, f.Form)
		}
		return nil, nil, fmt.Errorf("parent %q has no form %q (available: %s)", parentKey, formKey, strings.Join(available, ", "))
	}
	return nil, nil, fmt.Errorf("no parent %q in the registry", parentKey)
}
