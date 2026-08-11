// Command templetry-mcp is a Model Context Protocol server over the
// Templetry engine: stdio transport, newline-delimited JSON-RPC 2.0.
// It exposes the engine's verbs as tools so AI agents can browse the
// catalog, scaffold projects and run the update cycle (roadmap lane E).
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Templetry/engine/catalog"
	"github.com/Templetry/engine/manifest"
	"github.com/Templetry/engine/planner"
	"github.com/Templetry/engine/render"
	"github.com/Templetry/engine/source"
	"github.com/Templetry/engine/update"
)

// version is stamped by the release build via -ldflags "-X main.version=...".
var version = "1.2.0-dev"

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		line := bytes.TrimPrefix(in.Bytes(), []byte{0xEF, 0xBB, 0xBF})
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		res := handle(req)
		if res != nil { // notifications get no response
			_ = out.Encode(res)
		}
	}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func handle(req request) *response {
	if req.ID == nil {
		return nil // notification (e.g. notifications/initialized)
	}
	res := &response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		res.Result = map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "templetry", "version": version},
		}
	case "ping":
		res.Result = map[string]any{}
	case "tools/list":
		res.Result = map[string]any{"tools": toolDefs}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			res.Error = &rpcError{Code: -32602, Message: err.Error()}
			return res
		}
		text, err := callTool(p.Name, p.Arguments)
		if err != nil {
			res.Result = toolResult(err.Error(), true)
			return res
		}
		res.Result = toolResult(text, false)
	default:
		res.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return res
}

func toolResult(text string, isError bool) map[string]any {
	out := map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
	if isError {
		out["isError"] = true
	}
	return out
}

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var registryProp = map[string]any{
	"type":        "string",
	"description": "Registry URL or local file; omit for the official Templetry catalog",
}
var templateProp = map[string]any{
	"type":        "string",
	"description": "Template reference as <parent>/<form>, e.g. kmp/single-module (see list_templates)",
}
var variablesProp = map[string]any{
	"type":        "object",
	"description": "Variable values by key (see get_form_schema for keys, patterns and defaults)",
	"additionalProperties": map[string]any{"type": "string"},
}
var featuresProp = map[string]any{
	"type":        "object",
	"description": "Feature toggles by key",
	"additionalProperties": map[string]any{"type": "boolean"},
}
var presetProp = map[string]any{
	"type":        "string",
	"description": "Named feature combo from the manifest's presets; explicit features override it",
}

var toolDefs = []map[string]any{
	{
		"name":        "list_templates",
		"description": "List every template in the catalog: parents, forms, status and description.",
		"inputSchema": schema(map[string]any{"registry": registryProp}),
	},
	{
		"name":        "get_form_schema",
		"description": "Get a form's manifest as JSON: its variables (with patterns, options, defaults) and features. Call this before plan or render to know what inputs the template takes.",
		"inputSchema": schema(map[string]any{"template": templateProp, "registry": registryProp}, "template"),
	},
	{
		"name":        "plan",
		"description": "Dry-run: show exactly what rendering would produce (file actions, renames, exclusions) without writing anything.",
		"inputSchema": schema(map[string]any{
			"template": templateProp, "variables": variablesProp, "features": featuresProp,
			"preset": presetProp, "registry": registryProp,
		}, "template"),
	},
	{
		"name":        "render",
		"description": "Render a template into a local directory, producing a ready-to-work project (with its .templetry-answers.yml provenance record). The directory must be empty unless force is true.",
		"inputSchema": schema(map[string]any{
			"template": templateProp, "out_dir": map[string]any{"type": "string", "description": "Output directory"},
			"variables": variablesProp, "features": featuresProp, "preset": presetProp, "registry": registryProp,
			"force": map[string]any{"type": "boolean", "description": "Write into a non-empty directory"},
		}, "template", "out_dir"),
	},
	{
		"name":        "update",
		"description": "Template update cycle for an existing Templetry project: re-render at the template's head with the recorded inputs, diff against disk, three-way merge. Preview by default; set apply to write (never deletes user files).",
		"inputSchema": schema(map[string]any{
			"dir":   map[string]any{"type": "string", "description": "Project directory (contains .templetry-answers.yml)"},
			"apply": map[string]any{"type": "boolean", "description": "Write the update instead of previewing"},
		}, "dir"),
	},
}

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

// fetchForm resolves <parent>/<form> and downloads its template.
func fetchForm(registry, ref string) (*source.FileSet, *manifest.Manifest, string, error) {
	reg, err := loadRegistry(registry)
	if err != nil {
		return nil, nil, "", err
	}
	parent, form, err := reg.Resolve(ref)
	if err != nil {
		return nil, nil, "", err
	}
	files, err := source.FetchGitHubTarball(parent.Repo, parent.Ref, form.Path)
	if err != nil {
		return nil, nil, "", err
	}
	mf := files.Get("template.yml")
	if mf == nil {
		mf = files.Get("template.yaml")
	}
	if mf == nil {
		return nil, nil, "", fmt.Errorf("form %s has no template.yml", ref)
	}
	m, err := manifest.Load(mf.Data)
	if err != nil {
		return nil, nil, "", err
	}
	srcRef := fmt.Sprintf("github.com/%s@%s/%s", parent.Repo, parent.Ref, form.Path)
	return files, m, srcRef, nil
}

type toolArgs struct {
	Registry  string            `json:"registry"`
	Template  string            `json:"template"`
	OutDir    string            `json:"out_dir"`
	Variables map[string]string `json:"variables"`
	Features  map[string]bool   `json:"features"`
	Preset    string            `json:"preset"`
	Force     bool              `json:"force"`
	Dir       string            `json:"dir"`
	Apply     bool              `json:"apply"`
}

func callTool(name string, raw json.RawMessage) (string, error) {
	var a toolArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", err
		}
	}
	switch name {
	case "list_templates":
		reg, err := loadRegistry(a.Registry)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, p := range reg.Parents {
			fmt.Fprintf(&b, "%s — %s (%s@%s)\n", p.Key, p.Label, p.Repo, p.Ref)
			for _, f := range p.Forms {
				status := f.Status
				if status == "" {
					status = "ready"
				}
				fmt.Fprintf(&b, "  %-28s %-8s %s\n", p.Key+"/"+f.Form, status, f.Description)
			}
		}
		return b.String(), nil

	case "get_form_schema":
		_, m, _, err := fetchForm(a.Registry, a.Template)
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "plan":
		files, m, _, err := fetchForm(a.Registry, a.Template)
		if err != nil {
			return "", err
		}
		p, err := planner.Build(m, manifest.Inputs{Variables: a.Variables, Features: a.Features, Preset: a.Preset}, files)
		if err != nil {
			return "", err
		}
		return p.Describe(), nil

	case "render":
		if a.OutDir == "" {
			return "", fmt.Errorf("out_dir is required")
		}
		files, m, srcRef, err := fetchForm(a.Registry, a.Template)
		if err != nil {
			return "", err
		}
		p, err := planner.Build(m, manifest.Inputs{Variables: a.Variables, Features: a.Features, Preset: a.Preset}, files)
		if err != nil {
			return "", err
		}
		p.Source = srcRef
		rest := strings.TrimPrefix(srcRef, "github.com/")
		repo, right, _ := strings.Cut(rest, "@")
		ref, _, _ := strings.Cut(right, "/")
		if sha, err := source.ResolveGitHubRef(repo, ref, ""); err == nil {
			p.SourceCommit = sha
		}
		if entries, err := os.ReadDir(a.OutDir); err == nil && len(entries) > 0 && !a.Force {
			return "", fmt.Errorf("output directory %s is not empty (set force to override)", a.OutDir)
		}
		result, err := render.Apply(p, files)
		if err != nil {
			return "", err
		}
		if err := render.WriteDir(result, a.OutDir); err != nil {
			return "", err
		}
		return fmt.Sprintf("rendered %s -> %s (%d files)", a.Template, a.OutDir, result.Len()), nil

	case "update":
		if a.Dir == "" {
			return "", fmt.Errorf("dir is required")
		}
		p, err := update.Prepare(a.Dir, "")
		if err != nil {
			return "", err
		}
		short := func(s string) string {
			if len(s) > 7 {
				return s[:7]
			}
			return s
		}
		var b strings.Builder
		fmt.Fprintf(&b, "template %s · %s -> %s\n", p.Template, short(p.OldCommit), short(p.NewCommit))
		if len(p.Entries) == 0 {
			fmt.Fprintf(&b, "up to date (%d files unchanged)\n", p.Unchanged)
			return b.String(), nil
		}
		for _, e := range p.Entries {
			fmt.Fprintf(&b, "  %-9s %s\n", e.Status, e.Path)
		}
		fmt.Fprintf(&b, "  %d unchanged\n", p.Unchanged)
		if !a.Apply {
			fmt.Fprintf(&b, "preview only — call again with apply=true to write\n")
			return b.String(), nil
		}
		n, err := p.Apply()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "update applied: %d files written — review with git before committing\n", n)
		return b.String(), nil
	}
	return "", fmt.Errorf("unknown tool %q", name)
}
