package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Templetry/engine/catalog"
	"github.com/Templetry/engine/internal/netguard"
	"github.com/Templetry/engine/internal/safepath"
	"github.com/Templetry/engine/source"
)

// Environment variables the operator sets when launching the server. They
// are read once at start; no tool argument can change them. This is the
// line between operator and model: everything below is the operator's to
// decide, and everything a tool call carries (registry, out_dir, dir,
// answers files, template contents) is untrusted.
const (
	// EnvRoot is the workspace root: out_dir and dir must resolve inside
	// it. Default: the server's working directory.
	EnvRoot = "TEMPLETRY_MCP_ROOT"
	// EnvAllowedHosts extends the hosts the server may fetch registries and
	// templates from (comma-separated host names). The defaults are the
	// official registry and GitHub.
	EnvAllowedHosts = "TEMPLETRY_MCP_ALLOWED_HOSTS"
	// EnvRegistry is the registry used when a tool call names none: an
	// https URL or a local file. The operator may point it anywhere.
	EnvRegistry = "TEMPLETRY_MCP_REGISTRY"
)

// maxRegistryBytes bounds a registry download.
const maxRegistryBytes = 4 << 20

// settings is the operator's configuration of one server process.
type settings struct {
	root     string // absolute, symlinks resolved
	registry string // operator default registry; empty means the official one
	policy   netguard.Policy
	client   *http.Client
}

// current is the configuration the tools run under; main sets it before
// serving.
var current *settings

// newSettings reads the operator's configuration from args and the
// environment. A flag wins over its environment variable. base is the
// transport that actually connects (nil in production; tests inject one).
func newSettings(args []string, getenv func(string) string, cwd string, base http.RoundTripper, stderr io.Writer) (*settings, error) {
	fs := flag.NewFlagSet("templetry-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", getenv(EnvRoot), "workspace root; out_dir and dir must be inside it (env "+EnvRoot+"; default: working directory)")
	hosts := fs.String("allow-hosts", getenv(EnvAllowedHosts), "extra comma-separated hosts registries and templates may be fetched from (env "+EnvAllowedHosts+")")
	registry := fs.String("registry", getenv(EnvRegistry), "default registry: an https URL or a local file (env "+EnvRegistry+")")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	r := *root
	if r == "" {
		r = cwd
	}
	if !filepath.IsAbs(r) {
		r = filepath.Join(cwd, r)
	}
	real, err := filepath.EvalSymlinks(r)
	if err != nil {
		return nil, fmt.Errorf("workspace root %s: %w", r, err)
	}
	if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("workspace root %s is not a directory", real)
	}

	allowed := append(append([]string(nil), netguard.DefaultHosts...), netguard.ParseList(*hosts)...)
	if u, err := url.Parse(*registry); err == nil && u.Scheme == "https" && u.Host != "" {
		allowed = append(allowed, u.Hostname()) // the operator chose it
	}
	policy := netguard.NewPolicy(allowed)
	return &settings{
		root:     real,
		registry: *registry,
		policy:   policy,
		client:   policy.Client(base, 60*time.Second),
	}, nil
}

// resolveDir turns a tool's out_dir or dir argument into a directory inside
// the workspace root. Relative paths are relative to the root, not to the
// process. Symlinks are resolved first, so a link inside the root that
// points out of it is an escape, not a shortcut. Nothing under a .git
// directory is accepted either.
func (s *settings) resolveDir(arg string) (string, error) {
	if arg == "" || strings.ContainsRune(arg, 0) {
		return "", errors.New("empty or invalid directory")
	}
	p := arg
	if !filepath.IsAbs(p) {
		if filepath.VolumeName(p) != "" {
			return "", fmt.Errorf("directory %q is outside the workspace root", arg)
		}
		p = filepath.Join(s.root, p)
	}
	p = filepath.Clean(p)
	real, err := evalExisting(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(s.root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("directory %q is outside the workspace root %s (the operator sets the root with %s; tool arguments cannot)", arg, s.root, EnvRoot)
	}
	if rel != "." {
		if err := safepath.ValidateRel(filepath.ToSlash(rel)); err != nil {
			return "", fmt.Errorf("directory %q is not allowed: %w", arg, err)
		}
	}
	return real, nil
}

// evalExisting resolves symlinks in the longest prefix of p that exists and
// re-attaches the rest, so a directory that is about to be created is
// judged by where it would really land.
func evalExisting(p string) (string, error) {
	rest := ""
	cur := p
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(real, rest), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// loadRegistry reads the registry a tool call asked for.
//
//   - Omitted: the operator's default (EnvRegistry) or the official catalog.
//   - Equal to the operator's default: the same, trusted.
//   - Anything else came from the model, so it must be an https:// URL on an
//     allowlisted host. Never a local file, never http, never an IP or a
//     private address, never a redirect to another host.
func (s *settings) loadRegistry(location string) (*catalog.Registry, error) {
	if location == "" {
		location = s.registry
	}
	if location == "" {
		location = catalog.DefaultRegistryURL
	}
	if s.registry != "" && location == s.registry {
		if u, err := url.Parse(location); err == nil && u.Scheme == "https" {
			return s.fetchRegistry(u)
		}
		data, err := os.ReadFile(location) // operator-chosen file
		if err != nil {
			return nil, fmt.Errorf("reading the operator's registry: %w", err)
		}
		return catalog.Parse(data)
	}
	u, err := url.Parse(location)
	if err != nil {
		return nil, errors.New("registry rejected: not a valid URL")
	}
	if err := s.policy.CheckURL(u); err != nil {
		return nil, fmt.Errorf("registry rejected: %v (the MCP server only reads https:// registries on an allowed host; local files and other hosts are an operator setting, not a tool argument)", err)
	}
	return s.fetchRegistry(u)
}

func (s *settings) fetchRegistry(u *url.URL) (*catalog.Registry, error) {
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching registry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching registry: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRegistryBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRegistryBytes {
		return nil, fmt.Errorf("registry is larger than %d bytes", maxRegistryBytes)
	}
	return catalog.Parse(data)
}

// install makes s the running configuration, including the HTTP client every
// forge request uses. It returns a function that restores the previous one.
func (s *settings) install() (restore func()) {
	prev := current
	current = s
	source.SetHTTPClient(s.client)
	return func() {
		current = prev
		if prev != nil {
			source.SetHTTPClient(prev.client)
		} else {
			source.SetHTTPClient(nil)
		}
	}
}
