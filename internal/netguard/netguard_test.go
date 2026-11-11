package netguard

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestCheckURL(t *testing.T) {
	p := NewPolicy(append([]string{"Registry.Example."}, DefaultHosts...))
	ok := []string{
		"https://raw.githubusercontent.com/Templetry/catalog/main/registry.json",
		"https://github.com/x",
		"https://registry.example/r.json",
		"https://REGISTRY.example:443/r.json",
		"https://registry.example./r.json",
	}
	for _, s := range ok {
		if err := p.CheckURL(mustURL(t, s)); err != nil {
			t.Errorf("CheckURL(%s) = %v, want nil", s, err)
		}
	}
	bad := map[string]string{
		"file:///etc/passwd":                    "https",
		"/etc/passwd":                           "https",
		"registry.json":                         "https",
		"C:/Users/x/registry.json":              "https",
		"http://raw.githubusercontent.com/x":    "https",
		"ftp://github.com/x":                    "https",
		"https://127.0.0.1/x":                   "IP",
		"https://127.0.0.1:8443/x":              "port",
		"https://[::1]/x":                       "IP",
		"https://169.254.169.254/latest":        "IP",
		"https://10.0.0.5/x":                    "IP",
		"https://2130706433/x":                  "allowlist",
		"https://localhost/x":                   "localhost",
		"https://app.localhost/x":               "localhost",
		"https://evil.example/x":                "allowlist",
		"https://github.com.evil.example/x":     "allowlist",
		"https://evilgithub.com/x":              "allowlist",
		"https://registry.example:8443/x":       "port",
		"https://user:pw@github.com/x":          "credentials",
		"https://raw.githubusercontent.com:80/": "port",
	}
	for s, want := range bad {
		err := p.CheckURL(mustURL(t, s))
		if err == nil {
			t.Errorf("CheckURL(%s) = nil, want a refusal mentioning %q", s, want)
		} else if !strings.Contains(err.Error(), want) {
			t.Errorf("CheckURL(%s) = %v, want it to mention %q", s, err, want)
		}
	}
	// An IP literal stays refused even if an operator lists it.
	if err := NewPolicy([]string{"127.0.0.1"}).CheckURL(mustURL(t, "https://127.0.0.1/x")); err == nil {
		t.Error("an allowlisted IP literal must still be refused")
	}
}

func TestDisallowedIP(t *testing.T) {
	for _, s := range []string{
		"127.0.0.1", "127.1.2.3", "::1", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"169.254.169.254", "fe80::1", "fc00::1", "fd12::1", "0.0.0.0", "::", "100.64.0.1", "224.0.0.1",
		"ff02::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "198.18.0.1", "240.0.0.1", "255.255.255.255",
		"64:ff9b::7f00:1",
	} {
		if !DisallowedIP(netip.MustParseAddr(s)) {
			t.Errorf("DisallowedIP(%s) = false, want true", s)
		}
	}
	for _, s := range []string{"140.82.112.3", "185.199.108.133", "2606:50c0:8000::154", "8.8.8.8"} {
		if DisallowedIP(netip.MustParseAddr(s)) {
			t.Errorf("DisallowedIP(%s) = true, want false", s)
		}
	}
}

func TestParseList(t *testing.T) {
	got := strings.Join(ParseList(" A.example, ,b.example. ,"), "|")
	if got != "a.example|b.example" {
		t.Errorf("ParseList = %q", got)
	}
}

// A stub transport that records every request that gets past the guard.
type recorder struct {
	hits    []string
	handler http.Handler
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.hits = append(r.hits, req.URL.String())
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	res := rec.Result()
	res.Request = req
	return res, nil
}

func TestClientRefusesBeforeConnecting(t *testing.T) {
	rec := &recorder{handler: http.NotFoundHandler()}
	c := NewPolicy(DefaultHosts).Client(rec, time.Second)
	for _, s := range []string{"http://github.com/x", "https://evil.example/x", "https://127.0.0.1/x", "file:///etc/passwd"} {
		if _, err := c.Get(s); err == nil {
			t.Errorf("Get(%s) must be refused", s)
		}
	}
	if len(rec.hits) != 0 {
		t.Errorf("nothing may reach the network, got %v", rec.hits)
	}
}

// A redirect is a new request: even between two listed hosts it is refused,
// because the first host vouched for itself and not for the second.
func TestClientRefusesRedirectsToAnotherHost(t *testing.T) {
	rec := &recorder{}
	rec.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Host {
		case "github.com":
			http.Redirect(w, r, "https://api.github.com/other", http.StatusFound)
		case "api.github.com":
			_, _ = w.Write([]byte("reached"))
		}
	})
	c := NewPolicy(DefaultHosts).Client(rec, time.Second)
	_, err := c.Get("https://github.com/start")
	if err == nil || !strings.Contains(err.Error(), "different host") {
		t.Fatalf("redirect to another listed host = %v, want a refusal", err)
	}
	for _, h := range rec.hits {
		if strings.Contains(h, "api.github.com") {
			t.Errorf("the second host was contacted: %v", rec.hits)
		}
	}
}

func TestClientFollowsRedirectsOnTheSameHost(t *testing.T) {
	rec := &recorder{}
	rec.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	c := NewPolicy(DefaultHosts).Client(rec, time.Second)
	resp, err := c.Get("https://github.com/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(rec.hits) != 2 {
		t.Errorf("hits = %v", rec.hits)
	}
}

func TestClientRefusesRedirectToPlainHTTP(t *testing.T) {
	rec := &recorder{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://github.com/plain", http.StatusFound)
	})}
	c := NewPolicy(DefaultHosts).Client(rec, time.Second)
	if _, err := c.Get("https://github.com/start"); err == nil {
		t.Error("a downgrade to http must be refused")
	}
}

// The real transport refuses to connect to a loopback address even when the
// request got past the URL check — the defence against a name that resolves
// into the local network. A listening local server proves nothing arrives.
func TestTransportRefusesNonPublicAddresses(t *testing.T) {
	hits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()
	tr := NewTransport()
	req, _ := http.NewRequest("GET", srv.URL, nil)
	_, err := tr.RoundTrip(req)
	if err == nil || !strings.Contains(err.Error(), "non-public address") {
		t.Fatalf("RoundTrip to a loopback server = %v, want a refusal", err)
	}
	if hits != 0 {
		t.Error("the loopback server received a request")
	}
}
