// Package netguard is the outbound-network policy of the MCP server. There
// the registry and the template sources are chosen by a model, not by the
// operator, so every request must be https, go to a host the operator
// allowed, never to an address that is not on the public internet, and never
// follow a redirect to another host.
//
// The CLI does not use this package: a person typing --registry ./local is
// the operator, and that is an explicit action.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"syscall"
	"time"
)

// DefaultHosts are the hosts the engine itself talks to: the official
// registry (raw.githubusercontent.com) and GitHub's template downloads.
var DefaultHosts = []string{"raw.githubusercontent.com", "github.com", "api.github.com", "codeload.github.com"}

// Policy is an allowlist of host names.
type Policy struct {
	hosts map[string]bool
}

// NewPolicy builds a policy from host names (case-insensitive, no scheme or
// port). Blank entries are ignored.
func NewPolicy(hosts []string) Policy {
	p := Policy{hosts: map[string]bool{}}
	for _, h := range hosts {
		if h = normalize(h); h != "" {
			p.hosts[h] = true
		}
	}
	return p
}

// ParseList splits a comma-separated host list, as the operator writes it in
// an environment variable or flag.
func ParseList(s string) []string {
	var out []string
	for _, h := range strings.Split(s, ",") {
		if h = normalize(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// Hosts returns the allowed hosts, sorted.
func (p Policy) Hosts() []string {
	out := make([]string, 0, len(p.hosts))
	for h := range p.hosts {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func normalize(h string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
}

// CheckURL accepts u only when it is an https URL without credentials, on
// the default port, whose host is a name on the allowlist. IP literals and
// localhost are refused even if listed.
func (p Policy) CheckURL(u *url.URL) error {
	if u == nil {
		return errors.New("missing URL")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("only https:// URLs are allowed (got scheme %q)", u.Scheme)
	}
	if u.User != nil {
		return errors.New("URLs with credentials are not allowed")
	}
	host := normalize(u.Hostname())
	if host == "" {
		return errors.New("URL has no host")
	}
	if port := u.Port(); port != "" && port != "443" {
		return fmt.Errorf("port %s is not allowed (https on 443 only)", port)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return errors.New("IP addresses are not allowed, use a host name")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("localhost is not allowed")
	}
	if !p.hosts[host] {
		return fmt.Errorf("host %q is not on the allowlist (the operator can extend it with TEMPLETRY_MCP_ALLOWED_HOSTS)", host)
	}
	return nil
}

// CheckRedirect is an http.Client redirect policy: the target must pass
// CheckURL and be on the very same host as the first request.
func (p Policy) CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("stopped after 5 redirects")
	}
	first := via[0].URL
	if normalize(req.URL.Hostname()) != normalize(first.Hostname()) {
		return fmt.Errorf("refusing redirect from %s to a different host (%s)", first.Hostname(), req.URL.Hostname())
	}
	return p.CheckURL(req.URL)
}

type guardTransport struct {
	p    Policy
	base http.RoundTripper
}

func (g guardTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := g.p.CheckURL(r.URL); err != nil {
		return nil, err
	}
	return g.base.RoundTrip(r)
}

// Client returns an http.Client that applies the policy to every request and
// every redirect. base is the transport that actually connects; nil means
// NewTransport.
func (p Policy) Client(base http.RoundTripper, timeout time.Duration) *http.Client {
	if base == nil {
		base = NewTransport()
	}
	return &http.Client{Transport: guardTransport{p, base}, Timeout: timeout, CheckRedirect: p.CheckRedirect}
}

// DisallowedIP reports whether ip is not a public unicast address: loopback,
// private, link-local, carrier-grade NAT, unspecified, multicast, reserved
// and the other ranges that name the local network rather than the internet.
func DisallowedIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, c := range reservedNets {
		if c.Contains(ip) {
			return true
		}
	}
	return false
}

var reservedNets = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"64:ff9b::/96", "100::/64", "2001:db8::/32", "fec0::/10",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// NewTransport returns a transport that refuses to connect to a
// non-public address. The check runs on the address actually dialed, after
// DNS resolution, so a name that resolves into the local network — or is
// rebound there between a check and the connection — is refused.
//
// It does not use HTTP(S)_PROXY: through a proxy the dialed address is the
// proxy's, and the check would no longer see the real destination.
func NewTransport() *http.Transport {
	d := &net.Dialer{
		Timeout: 15 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if DisallowedIP(ip) {
				return fmt.Errorf("refusing to connect to non-public address %s", ip)
			}
			return nil
		},
	}
	return &http.Transport{
		DialContext:           func(ctx context.Context, n, a string) (net.Conn, error) { return d.DialContext(ctx, n, a) },
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
}
