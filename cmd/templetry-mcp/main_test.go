package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Templetry/engine/internal/forgetest"
)

const demoManifest = `schema_version: 1
name: demo
description: A tiny demo template
variables:
  - key: project_name
    type: string
    pattern: "^[A-Za-z][A-Za-z0-9 ]*$"
identity:
  - from: "template-app"
    to: "{project_name.kebab}"
features:
  - key: docs
    label: Docs folder
    default: false
    files: ["docs/**"]
`

func demoForm(readme string) map[string]string {
	return map[string]string{
		"forms/demo/template.yml":               demoManifest,
		"forms/demo/README.md":                  readme,
		"forms/demo/src/template-app.go":        "package main // template-app\n",
		"forms/demo/docs/guide.md":              "guide\n",
		"forms/demo/pieces/ci/piece.yml":        "schema_version: 1\nname: ci\ndescription: CI workflow\n",
		"forms/demo/pieces/ci/.ci/template-app": "build template-app\n",
	}
}

// fixture publishes the demo form on a fake forge and writes a registry
// file that points at it.
func fixture(t *testing.T) (*forgetest.Forge, string) {
	t.Helper()
	forge := forgetest.Install(t)
	forge.Publish(t, "acme/templates", "main", "1111111aaaa", demoForm("# template-app\n"))
	reg := filepath.Join(t.TempDir(), "registry.json")
	data := `{"schema_version": 2, "parents": [
	  {"key": "acme", "label": "Acme", "repo": "acme/templates", "ref": "main", "forms": [
	    {"form": "demo", "name": "demo", "path": "forms/demo", "status": "ready", "description": "Demo form"},
	    {"form": "later", "name": "later", "path": "forms/later", "status": "planned", "description": "Not yet"}
	  ]}
	]}`
	if err := os.WriteFile(reg, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return forge, reg
}

type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

// exchange feeds lines to the server as a client would over stdio and
// returns every reply in order.
func exchange(t *testing.T, lines ...string) []rpcReply {
	t.Helper()
	var out strings.Builder
	serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out)
	replies := []rpcReply{}
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for sc.Scan() {
		var r rpcReply
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("server wrote a non-JSON line %q: %v", sc.Text(), err)
		}
		if r.JSONRPC != "2.0" {
			t.Errorf("reply without jsonrpc 2.0: %s", sc.Text())
		}
		replies = append(replies, r)
	}
	return replies
}

func call(id int, tool string, args map[string]any) string {
	params, _ := json.Marshal(map[string]any{"name": tool, "arguments": args})
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":%s}`, id, params)
}

type toolReply struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

func toolText(t *testing.T, r rpcReply) (string, bool) {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("tools/call failed at the protocol level: %+v", r.Error)
	}
	var tr toolReply
	if err := json.Unmarshal(r.Result, &tr); err != nil {
		t.Fatal(err)
	}
	if len(tr.Content) != 1 || tr.Content[0].Type != "text" {
		t.Fatalf("tool result content = %+v", tr.Content)
	}
	return tr.Content[0].Text, tr.IsError
}

func TestHandshakeAndToolList(t *testing.T) {
	replies := exchange(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"two","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	)
	if len(replies) != 3 {
		t.Fatalf("got %d replies, want 3 (the notification must not be answered)", len(replies))
	}

	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools map[string]any `json:"tools"`
		} `json:"capabilities"`
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if string(replies[0].ID) != "1" {
		t.Errorf("initialize id = %s", replies[0].ID)
	}
	if err := json.Unmarshal(replies[0].Result, &init); err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != "2025-03-26" {
		t.Errorf("server must echo the client's protocol version, got %q", init.ProtocolVersion)
	}
	if init.Capabilities.Tools == nil || init.ServerInfo.Name != "templetry" || init.ServerInfo.Version != version {
		t.Errorf("initialize result = %s", replies[0].Result)
	}

	if string(replies[1].ID) != `"two"` {
		t.Errorf("string ids must round-trip, got %s", replies[1].ID)
	}
	var list struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema struct {
				Type       string         `json:"type"`
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(replies[1].Result, &list); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" || tool.InputSchema.Type != "object" {
			t.Errorf("tool %s is not fully described: %+v", tool.Name, tool)
		}
		for _, req := range tool.InputSchema.Required {
			if _, ok := tool.InputSchema.Properties[req]; !ok {
				t.Errorf("tool %s requires undeclared property %q", tool.Name, req)
			}
		}
	}
	want := "list_templates get_form_schema plan render update list_pieces add_piece"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("tools = %s, want %s", got, want)
	}

	if string(replies[2].Result) != "{}" {
		t.Errorf("ping result = %s", replies[2].Result)
	}
}

func TestInitializeDefaultsTheProtocolVersion(t *testing.T) {
	replies := exchange(t, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if len(replies) != 1 || !strings.Contains(string(replies[0].Result), `"protocolVersion":"2025-06-18"`) {
		t.Errorf("initialize without params = %+v", replies)
	}
}

func TestReadOnlyToolsAgainstAFixture(t *testing.T) {
	forge, reg := fixture(t)
	replies := exchange(t,
		call(1, "list_templates", map[string]any{"registry": reg}),
		call(2, "get_form_schema", map[string]any{"registry": reg, "template": "acme/demo"}),
		call(3, "plan", map[string]any{
			"registry": reg, "template": "acme/demo",
			"variables": map[string]string{"project_name": "Hello World"},
		}),
		call(4, "plan", map[string]any{
			"registry": reg, "template": "acme/demo",
			"variables": map[string]string{"project_name": "Hello World"},
			"features":  map[string]bool{"docs": true},
		}),
	)
	if len(replies) != 4 {
		t.Fatalf("got %d replies", len(replies))
	}

	text, isErr := toolText(t, replies[0])
	if isErr || !strings.Contains(text, "acme — Acme (acme/templates@main)") ||
		!strings.Contains(text, "acme/demo") || !strings.Contains(text, "planned") {
		t.Errorf("list_templates = %q (isError=%v)", text, isErr)
	}

	text, isErr = toolText(t, replies[1])
	var schema struct {
		Name      string `json:"name"`
		Variables []struct {
			Key string `json:"key"`
		} `json:"variables"`
	}
	if isErr || json.Unmarshal([]byte(text), &schema) != nil || schema.Name != "demo" ||
		len(schema.Variables) != 1 || schema.Variables[0].Key != "project_name" {
		t.Errorf("get_form_schema = %q (isError=%v)", text, isErr)
	}

	plain, isErr := toolText(t, replies[2])
	if isErr || !strings.Contains(plain, "src/template-app.go -> src/hello-world.go") || !strings.Contains(plain, "exclude pieces/ci/piece.yml (piece)") {
		t.Errorf("plan = %q (isError=%v)", plain, isErr)
	}
	withDocs, _ := toolText(t, replies[3])
	if plain == withDocs {
		t.Error("a feature toggle must change the plan")
	}

	// Read-only tools fetch, but never write; and nothing reached past the
	// fake forge.
	for _, u := range forge.Requests {
		if !strings.HasPrefix(u, "https://codeload.github.com/") && !strings.HasPrefix(u, "https://api.github.com/") {
			t.Errorf("unexpected request %s", u)
		}
	}
}

func TestWritingToolsRoundTrip(t *testing.T) {
	forge, reg := fixture(t)
	out := filepath.Join(t.TempDir(), "proj")
	replies := exchange(t,
		call(1, "render", map[string]any{
			"registry": reg, "template": "acme/demo", "out_dir": out,
			"variables": map[string]string{"project_name": "Hello World"},
		}),
		call(2, "render", map[string]any{
			"registry": reg, "template": "acme/demo", "out_dir": out,
			"variables": map[string]string{"project_name": "Hello World"},
		}),
		call(3, "list_pieces", map[string]any{"dir": out}),
		call(4, "add_piece", map[string]any{"dir": out, "piece": "ci"}),
		call(5, "add_piece", map[string]any{"dir": out, "piece": "ci"}),
		call(6, "update", map[string]any{"dir": out}),
	)
	if len(replies) != 6 {
		t.Fatalf("got %d replies", len(replies))
	}
	if text, isErr := toolText(t, replies[0]); isErr || !strings.Contains(text, "rendered acme/demo -> ") {
		t.Errorf("render = %q", text)
	}
	if data, err := os.ReadFile(filepath.Join(out, "src", "hello-world.go")); err != nil || !strings.Contains(string(data), "hello-world") {
		t.Errorf("rendered file = %q, %v", data, err)
	}
	if ans, err := os.ReadFile(filepath.Join(out, ".templetry-answers.yml")); err != nil ||
		!strings.Contains(string(ans), "commit: 1111111aaaa") || !strings.Contains(string(ans), "github.com/acme/templates@main/forms/demo") {
		t.Errorf("answers = %s, %v", ans, err)
	}
	if text, isErr := toolText(t, replies[1]); !isErr || !strings.Contains(text, "not empty") {
		t.Errorf("render into a non-empty dir = %q (isError=%v)", text, isErr)
	}
	if text, _ := toolText(t, replies[2]); !strings.Contains(text, "ci") || !strings.Contains(text, "CI workflow") {
		t.Errorf("list_pieces = %q", text)
	}
	if text, isErr := toolText(t, replies[3]); isErr || !strings.Contains(text, "piece ci applied: 1 files") {
		t.Errorf("add_piece = %q", text)
	}
	if _, err := os.Stat(filepath.Join(out, ".ci", "hello-world")); err != nil {
		t.Errorf("piece file not written: %v", err)
	}
	if text, isErr := toolText(t, replies[4]); !isErr || !strings.Contains(text, "already applied") {
		t.Errorf("second add_piece = %q (isError=%v)", text, isErr)
	}
	if text, _ := toolText(t, replies[5]); !strings.Contains(text, "up to date") {
		t.Errorf("update right after render = %q", text)
	}

	// The template moves on; update previews, then applies.
	forge.Publish(t, "acme/templates", "main", "2222222bbbb", demoForm("# template-app v2\n"))
	replies = exchange(t, call(7, "update", map[string]any{"dir": out}))
	preview, _ := toolText(t, replies[0])
	if !strings.Contains(preview, "1111111 -> 2222222") || !strings.Contains(preview, "modified  README.md") ||
		!strings.Contains(preview, "preview only") {
		t.Errorf("update preview = %q", preview)
	}
	if data, _ := os.ReadFile(filepath.Join(out, "README.md")); string(data) != "# hello-world\n" {
		t.Errorf("preview wrote to disk: %q", data)
	}
	replies = exchange(t, call(8, "update", map[string]any{"dir": out, "apply": true}))
	applied, _ := toolText(t, replies[0])
	if !strings.Contains(applied, "update applied: 1 files written") {
		t.Errorf("update apply = %q", applied)
	}
	if data, _ := os.ReadFile(filepath.Join(out, "README.md")); string(data) != "# hello-world v2\n" {
		t.Errorf("README after update = %q", data)
	}
}

func TestInvalidRequests(t *testing.T) {
	_, reg := fixture(t)
	replies := exchange(t,
		`this is not json`,
		"\xEF\xBB\xBF"+`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		``,
		`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":"not an object"}`,
		call(4, "no_such_tool", nil),
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"render","arguments":{"force":"yes"}}}`,
		call(6, "render", map[string]any{"template": "acme/demo"}),
		call(7, "update", map[string]any{}),
		call(8, "list_pieces", map[string]any{}),
		call(9, "add_piece", map[string]any{"dir": "x"}),
		call(10, "get_form_schema", map[string]any{"registry": reg, "template": "acme/later"}),
		call(11, "plan", map[string]any{"registry": reg, "template": "acme/demo", "variables": map[string]string{"project_name": "1 bad"}}),
		call(12, "list_templates", map[string]any{"registry": filepath.Join(t.TempDir(), "missing.json")}),
		call(13, "update", map[string]any{"dir": t.TempDir()}),
	)
	if len(replies) != 13 {
		ids := []string{}
		for _, r := range replies {
			ids = append(ids, string(r.ID))
		}
		t.Fatalf("got %d replies %v, want 13 (garbage and blank lines are skipped, the rest answered)", len(replies), ids)
	}
	if string(replies[0].ID) != "1" || replies[0].Error != nil {
		t.Errorf("a BOM-prefixed request must still be served: %+v", replies[0])
	}
	if e := replies[1].Error; e == nil || e.Code != -32601 || !strings.Contains(e.Message, "resources/list") {
		t.Errorf("unknown method = %+v", replies[1])
	}
	if e := replies[2].Error; e == nil || e.Code != -32602 {
		t.Errorf("malformed tools/call params = %+v", replies[2])
	}
	wantErr := map[int]string{
		3:  `unknown tool "no_such_tool"`,
		4:  "cannot unmarshal",
		5:  "out_dir is required",
		6:  "dir is required",
		7:  "dir is required",
		8:  "dir and piece are required",
		9:  "not ready yet",
		10: "project_name",
		11: "registry",
		12: "no answers file",
	}
	for i, want := range wantErr {
		text, isErr := toolText(t, replies[i])
		if !isErr || !strings.Contains(text, want) {
			t.Errorf("reply %s = %q (isError=%v), want an error mentioning %q", replies[i].ID, text, isErr, want)
		}
	}
}
