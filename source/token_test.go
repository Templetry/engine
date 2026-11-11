package source

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Templetry/engine/internal/forgetest"
)

// hostRouter is an in-process network: every request goes to the handler
// registered for its host, redirects included, and the credentials each host
// saw are recorded. It plugs in through http.DefaultClient.Transport, the
// same seam forgetest uses, so no socket is opened.
type hostRouter struct {
	mu       sync.Mutex
	handlers map[string]http.Handler
	seen     map[string][]http.Header // host -> headers of each request it received
}

func installRouter(t *testing.T, handlers map[string]http.Handler) *hostRouter {
	t.Helper()
	h := &hostRouter{handlers: handlers, seen: map[string][]http.Header{}}
	prev := http.DefaultClient.Transport
	http.DefaultClient.Transport = h
	t.Cleanup(func() { http.DefaultClient.Transport = prev })
	return h
}

func (h *hostRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	h.mu.Lock()
	h.seen[host] = append(h.seen[host], req.Header.Clone())
	h.mu.Unlock()
	handler, ok := h.handlers[host]
	if !ok {
		return &http.Response{StatusCode: 502, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	res := rec.Result()
	res.Request = req
	return res, nil
}

func (h *hostRouter) saw(host, header string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, hd := range h.seen[host] {
		if v := hd.Get(header); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (h *hostRouter) requests(host string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.seen[host])
}

func tarballHandler(t *testing.T) http.Handler {
	t.Helper()
	data, err := forgetest.Tarball("acme/templates", map[string]string{"README.md": "hi\n"})
	if err != nil {
		t.Fatal(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) })
}

func redirectTo(target string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	})
}

const secret = "ghp_test-token-value"

// A registry or an answers file names the forge host, so a token must not
// follow it: it was sent to whatever host the source named.
func TestTokenIsNotSentToAnUnlistedHost(t *testing.T) {
	t.Setenv(TokenHostsEnv, "")
	router := installRouter(t, map[string]http.Handler{"evil.example": tarballHandler(t)})
	for _, ref := range []string{"gitlab:evil.example/acme/templates", "gitea:evil.example/acme/templates"} {
		src, err := ParseRef(ref)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Fetch(src, "main", "", secret); err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		_, _ = ResolveRef(src, "main", secret) // the stub serves tarballs only; the request is what matters
	}
	for _, hd := range []string{"Authorization", "PRIVATE-TOKEN"} {
		if got := router.saw("evil.example", hd); len(got) != 0 {
			t.Errorf("evil.example received %s: %v", hd, got)
		}
	}
	if router.requests("evil.example") == 0 {
		t.Fatal("the test never reached the host")
	}
}

func TestTokenGoesToGitHubHosts(t *testing.T) {
	t.Setenv(TokenHostsEnv, "")
	router := installRouter(t, map[string]http.Handler{
		"codeload.github.com": tarballHandler(t),
		"api.github.com":      http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("abc123")) }),
	})
	src, _ := ParseRef("acme/templates")
	if _, err := Fetch(src, "main", "", secret); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveRef(src, "main", secret); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"codeload.github.com", "api.github.com"} {
		if got := router.saw(host, "Authorization"); len(got) != 1 || got[0] != "Bearer "+secret {
			t.Errorf("%s should receive the token once, got %v", host, got)
		}
	}
}

// The operator can add a host (a self-hosted GitLab) to the list.
func TestTokenHostsEnvExtendsTheList(t *testing.T) {
	t.Setenv(TokenHostsEnv, " Git.Corp.Example , other.example")
	router := installRouter(t, map[string]http.Handler{"git.corp.example": tarballHandler(t)})
	src, _ := ParseRef("gitlab:git.corp.example/acme/templates")
	if _, err := Fetch(src, "main", "", secret); err != nil {
		t.Fatal(err)
	}
	if got := router.saw("git.corp.example", "PRIVATE-TOKEN"); len(got) != 1 || got[0] != secret {
		t.Errorf("listed host should receive the token, got %v", got)
	}
}

// net/http strips Authorization on a cross-domain redirect but forwards any
// other header, so GitLab's PRIVATE-TOKEN leaked to wherever a listed host
// redirected. The redirect must arrive without any credential.
func TestTokenIsStrippedOnARedirectOffTheList(t *testing.T) {
	t.Setenv(TokenHostsEnv, "forge.example")
	for _, ref := range []string{"gitlab:forge.example/acme/templates", "gitea:forge.example/acme/templates"} {
		router := installRouter(t, map[string]http.Handler{
			"forge.example": redirectTo("https://evil.example/stolen.tar.gz"),
			"evil.example":  tarballHandler(t),
		})
		src, _ := ParseRef(ref)
		if _, err := Fetch(src, "main", "", secret); err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		if router.requests("evil.example") != 1 {
			t.Fatalf("%s: the redirect was not followed, so nothing was tested", ref)
		}
		for _, hd := range []string{"Authorization", "PRIVATE-TOKEN"} {
			if got := router.saw("evil.example", hd); len(got) != 0 {
				t.Errorf("%s: redirect target received %s: %v", ref, hd, got)
			}
		}
	}
}

func TestTokenIsNeverSentOverPlainHTTP(t *testing.T) {
	t.Setenv(TokenHostsEnv, "forge.example")
	router := installRouter(t, map[string]http.Handler{
		"forge.example": redirectTo("http://forge.example/plain.tar.gz"),
	})
	src, _ := ParseRef("gitlab:forge.example/acme/templates")
	_, _ = Fetch(src, "main", "", secret)
	for _, hd := range router.seen["forge.example"][1:] {
		if hd.Get("PRIVATE-TOKEN") != "" || hd.Get("Authorization") != "" {
			t.Error("the token followed a redirect to plain http")
		}
	}
}
