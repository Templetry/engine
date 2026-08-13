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
