package source

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// TokenHostsEnv names the environment variable an operator uses to extend
// the hosts that may receive TEMPLETRY_TOKEN: a comma-separated list of host
// names (no scheme, no port).
const TokenHostsEnv = "TEMPLETRY_TOKEN_HOSTS"

// tokenHosts are the hosts the token is sent to by default: the GitHub hosts
// the engine itself talks to. A token for another forge (GitLab, Gitea)
// travels only after the operator lists that host in TokenHostsEnv.
var tokenHosts = []string{"github.com", "api.github.com", "codeload.github.com"}

// TokenHosts returns every host that may receive the token right now.
func TokenHosts() []string {
	out := append([]string(nil), tokenHosts...)
	for _, h := range strings.Split(os.Getenv(TokenHostsEnv), ",") {
		if h = normHost(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func normHost(h string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
}

// tokenAllowed reports whether a credential may be attached to a request for
// u. A registry or an answers file can name any forge host, so the host a
// request goes to — not where the token came from — decides. Plain http is
// never allowed.
func tokenAllowed(u *url.URL) bool {
	if u == nil || u.Scheme != "https" {
		return false
	}
	host := normHost(u.Hostname())
	for _, h := range TokenHosts() {
		if h == host {
			return true
		}
	}
	return false
}

// tokenHeaders are every header name a token can travel in. net/http only
// strips Authorization (and Cookie) on a cross-domain redirect; the others
// would be forwarded, so they are removed explicitly.
var tokenHeaders = []string{"Authorization", "PRIVATE-TOKEN"}

// httpClient is what every forge request goes through; nil means
// http.DefaultClient. The MCP server swaps in a client that enforces a host
// allowlist, because there the registry and the answers file are chosen by a
// model, not by the operator.
var httpClient *http.Client

// SetHTTPClient replaces the client used for every forge request. Call it
// before any request is made; nil restores http.DefaultClient.
func SetHTTPClient(c *http.Client) { httpClient = c }

// do sends a forge request. Whatever the client's own redirect policy says,
// a redirect never carries a token to a host that is not allowed to have it.
func do(req *http.Request) (*http.Response, error) {
	base := httpClient
	if base == nil {
		base = http.DefaultClient
	}
	c := *base
	inner := base.CheckRedirect
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if !tokenAllowed(r.URL) {
			for _, h := range tokenHeaders {
				r.Header.Del(h)
			}
		}
		if inner != nil {
			return inner(r, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return c.Do(req)
}

// setAuth attaches the token to req when, and only when, req's host may
// receive it.
func (r Ref) setAuth(req *http.Request, token string) {
	if !tokenAllowed(req.URL) {
		return
	}
	if k, v := r.authHeader(token); k != "" {
		req.Header.Set(k, v)
	}
}
