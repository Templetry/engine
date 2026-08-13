package source

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Ref is a parsed template source: which forge, which host, which repo.
// The textual form is "<scheme>:<host>/<path>" — or a bare "owner/repo",
// which means GitHub (backwards compatible with every existing answers
// file and registry entry).
//
//	github:owner/repo
//	gitlab:gitlab.com/group/subgroup/project
//	gitea:codeberg.org/owner/repo
type Ref struct {
	Scheme string // github | gitlab | gitea
	Host   string // api host, e.g. github.com, gitlab.com, codeberg.org
	Repo   string // owner/name (GitLab allows nested groups)
}

// ParseRef reads a source reference. Unknown schemes are an error naming
// the supported ones.
func ParseRef(s string) (Ref, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Ref{}, fmt.Errorf("empty source reference")
	}
	scheme, rest, hasScheme := strings.Cut(s, ":")
	if !hasScheme {
		return Ref{Scheme: "github", Host: "github.com", Repo: s}, nil
	}
	rest = strings.TrimPrefix(rest, "//")
	switch scheme {
	case "github":
		// Host is fixed: GitHub Enterprise would need its own scheme.
		if host, repo, ok := splitHostRepo(rest); ok && host == "github.com" {
			return Ref{Scheme: "github", Host: host, Repo: repo}, nil
		}
		return Ref{Scheme: "github", Host: "github.com", Repo: rest}, nil
	case "gitlab", "gitea":
		host, repo, ok := splitHostRepo(rest)
		if !ok {
			return Ref{}, fmt.Errorf("%s source needs host/owner/repo, got %q", scheme, rest)
		}
		return Ref{Scheme: scheme, Host: host, Repo: repo}, nil
	default:
		return Ref{}, fmt.Errorf("unknown source scheme %q (supported: github, gitlab, gitea)", scheme)
	}
}

// splitHostRepo splits "host/owner/name" when the first segment looks like
// a host (contains a dot or a port) and at least two segments follow.
func splitHostRepo(s string) (host, repo string, ok bool) {
	first, rest, found := strings.Cut(s, "/")
	if !found || rest == "" {
		return "", "", false
	}
	if !strings.ContainsAny(first, ".:") || !strings.Contains(rest, "/") {
		return "", "", false
	}
	return first, rest, true
}

// ParseSourceString reads the form the answers file records:
// "<ref-string>@<git-ref>[/<subdir>]" — where ref-string is a scheme'd
// reference or the legacy host-prefixed GitHub form.
//
//	github.com/Templetry/web@main/react-spa      (legacy, still valid)
//	gitlab:gitlab.com/group/proj@main/starter
func ParseSourceString(s string) (Ref, string, string, error) {
	if s == "" || s == "local" {
		return Ref{}, "", "", fmt.Errorf("project has no remote template source (rendered from a local directory)")
	}
	left, right, ok := strings.Cut(s, "@")
	if !ok {
		return Ref{}, "", "", fmt.Errorf("cannot parse source %q (expected <source>@<ref>[/<subdir>])", s)
	}
	// Legacy: bare "github.com/owner/repo" carries the host, no scheme.
	if !strings.Contains(left, ":") {
		left = strings.TrimPrefix(left, "github.com/")
	}
	src, err := ParseRef(left)
	if err != nil {
		return Ref{}, "", "", err
	}
	gitRef, subdir, _ := strings.Cut(right, "/")
	return src, gitRef, subdir, nil
}

// FormatSource builds the string the answers file records for a render.
func FormatSource(src Ref, gitRef, subdir string) string {
	out := src.String()
	if src.Scheme == "github" {
		out = "github.com/" + src.Repo // keep the historical shape for GitHub
	}
	out += "@" + gitRef
	if subdir != "" {
		out += "/" + subdir
	}
	return out
}

// String renders the canonical textual form.
func (r Ref) String() string {
	if r.Scheme == "github" {
		return "github:" + r.Repo
	}
	return r.Scheme + ":" + r.Host + "/" + r.Repo
}

// tarballURL is where the forge serves a gzipped archive of a ref.
func (r Ref) tarballURL(ref string) string {
	switch r.Scheme {
	case "gitlab":
		// GitLab wants the project path URL-encoded in the API route.
		return fmt.Sprintf("https://%s/api/v4/projects/%s/repository/archive.tar.gz?sha=%s",
			r.Host, url.PathEscape(r.Repo), url.QueryEscape(ref))
	case "gitea":
		return fmt.Sprintf("https://%s/api/v1/repos/%s/archive/%s.tar.gz",
			r.Host, r.Repo, url.PathEscape(ref))
	default:
		return fmt.Sprintf("https://codeload.%s/%s/tar.gz/%s", r.Host, r.Repo, ref)
	}
}

// authHeader is the header name/value a token travels in for this forge.
func (r Ref) authHeader(token string) (string, string) {
	if token == "" {
		return "", ""
	}
	switch r.Scheme {
	case "gitlab":
		return "PRIVATE-TOKEN", token
	case "gitea":
		return "Authorization", "token " + token
	default:
		return "Authorization", "Bearer " + token
	}
}

// Fetch downloads the template at ref as a FileSet, optionally scoped to
// subdir. token may be empty for public repositories.
func Fetch(src Ref, ref, subdir, token string) (*FileSet, error) {
	req, err := http.NewRequest("GET", src.tarballURL(ref), nil)
	if err != nil {
		return nil, err
	}
	if k, v := src.authHeader(token); k != "" {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s@%s: %w", src, ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s@%s: HTTP %d", src, ref, resp.StatusCode)
	}
	return FromTarGz(resp.Body, subdir)
}

// ResolveRef resolves a branch, tag or ref to its commit SHA — the drift
// anchor the answers file records.
func ResolveRef(src Ref, ref, token string) (string, error) {
	switch src.Scheme {
	case "github":
		return ResolveGitHubRef(src.Repo, ref, token)
	case "gitlab":
		var out struct {
			ID string `json:"id"`
		}
		u := fmt.Sprintf("https://%s/api/v4/projects/%s/repository/commits/%s",
			src.Host, url.PathEscape(src.Repo), url.PathEscape(ref))
		if err := getJSON(src, u, token, &out); err != nil {
			return "", err
		}
		return out.ID, nil
	case "gitea":
		var out []struct {
			SHA string `json:"sha"`
		}
		u := fmt.Sprintf("https://%s/api/v1/repos/%s/commits?sha=%s&limit=1&stat=false&verification=false&files=false",
			src.Host, src.Repo, url.QueryEscape(ref))
		if err := getJSON(src, u, token, &out); err != nil {
			return "", err
		}
		if len(out) == 0 {
			return "", fmt.Errorf("resolving %s@%s: no commits", src, ref)
		}
		return out[0].SHA, nil
	}
	return "", fmt.Errorf("unknown scheme %q", src.Scheme)
}

func getJSON(src Ref, u, token string, v any) error {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	if k, hv := src.authHeader(token); k != "" {
		req.Header.Set(k, hv)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
