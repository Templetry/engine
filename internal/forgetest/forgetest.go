// Package forgetest is an in-memory GitHub stand-in for tests: it serves
// the two endpoints the engine talks to (codeload tarballs and the commits
// API that resolves a ref to a SHA) through http.DefaultClient, so the
// fetch → render → update pipeline runs end to end without the network.
package forgetest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Forge holds published template snapshots keyed by repo and ref.
type Forge struct {
	mu       sync.Mutex
	tarballs map[string][]byte // "owner/name@ref" -> tar.gz
	commits  map[string]string // "owner/name@ref" -> sha
	Requests []string          // every URL served, for assertions
}

// Install swaps http.DefaultClient's transport for the forge until the
// test ends. Tests that install a forge must not run in parallel.
func Install(t testing.TB) *Forge {
	t.Helper()
	f := &Forge{tarballs: map[string][]byte{}, commits: map[string]string{}}
	prev := http.DefaultClient.Transport
	http.DefaultClient.Transport = f
	t.Cleanup(func() { http.DefaultClient.Transport = prev })
	return f
}

// Publish makes files available at ref (a branch or tag) and at commit,
// and makes ref resolve to commit — what a push to a branch looks like.
func (f *Forge) Publish(t testing.TB, repo, ref, commit string, files map[string]string) {
	t.Helper()
	data, err := Tarball(repo, files)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tarballs[repo+"@"+ref] = data
	f.tarballs[repo+"@"+commit] = data
	f.commits[repo+"@"+ref] = commit
	f.commits[repo+"@"+commit] = commit
}

// RoundTrip implements http.RoundTripper.
func (f *Forge) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, req.URL.String())
	host, p := req.URL.Host, strings.TrimPrefix(req.URL.Path, "/")
	switch host {
	case "codeload.github.com":
		// owner/name/tar.gz/ref
		parts := strings.SplitN(p, "/", 4)
		if len(parts) == 4 && parts[2] == "tar.gz" {
			if data, ok := f.tarballs[parts[0]+"/"+parts[1]+"@"+parts[3]]; ok {
				return reply(req, http.StatusOK, data), nil
			}
		}
	case "api.github.com":
		// repos/owner/name/commits/ref
		parts := strings.SplitN(p, "/", 5)
		if len(parts) == 5 && parts[0] == "repos" && parts[3] == "commits" {
			if sha, ok := f.commits[parts[1]+"/"+parts[2]+"@"+parts[4]]; ok {
				return reply(req, http.StatusOK, []byte(sha)), nil
			}
		}
	}
	return reply(req, http.StatusNotFound, []byte("not found")), nil
}

func reply(req *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

// Tarball builds a GitHub-shaped archive: every file under one wrapper
// directory, which the engine strips.
func Tarball(repo string, files map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	wrapper := strings.ReplaceAll(repo, "/", "-") + "-snapshot/"
	for _, p := range paths {
		data := []byte(files[p])
		if err := tw.WriteHeader(&tar.Header{
			Name: wrapper + p, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg,
		}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
