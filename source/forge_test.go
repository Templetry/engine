package source

import "testing"

func TestParseRef(t *testing.T) {
	cases := []struct {
		in     string
		scheme string
		host   string
		repo   string
	}{
		{"Templetry/web", "github", "github.com", "Templetry/web"},
		{"github:Templetry/web", "github", "github.com", "Templetry/web"},
		{"github:github.com/Templetry/web", "github", "github.com", "Templetry/web"},
		{"gitlab:gitlab.com/group/proj", "gitlab", "gitlab.com", "group/proj"},
		{"gitlab:gitlab.com/group/sub/proj", "gitlab", "gitlab.com", "group/sub/proj"},
		{"gitea:codeberg.org/owner/repo", "gitea", "codeberg.org", "owner/repo"},
		{"gitea:git.example.com:3000/owner/repo", "gitea", "git.example.com:3000", "owner/repo"},
	}
	for _, c := range cases {
		got, err := ParseRef(c.in)
		if err != nil {
			t.Errorf("ParseRef(%q): %v", c.in, err)
			continue
		}
		if got.Scheme != c.scheme || got.Host != c.host || got.Repo != c.repo {
			t.Errorf("ParseRef(%q) = %+v, want {%s %s %s}", c.in, got, c.scheme, c.host, c.repo)
		}
	}
	for _, bad := range []string{"", "svn:host/a/b", "gitlab:nohost"} {
		if _, err := ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) must fail", bad)
		}
	}
}

func TestParseSourceStringLegacyAndScheme(t *testing.T) {
	// Legacy answers files must keep working unchanged (ADR-0015).
	src, ref, sub, err := ParseSourceString("github.com/Templetry/web@main/react-spa")
	if err != nil || src.Scheme != "github" || src.Repo != "Templetry/web" || ref != "main" || sub != "react-spa" {
		t.Errorf("legacy parse = %+v %q %q err=%v", src, ref, sub, err)
	}
	src, ref, sub, err = ParseSourceString("gitlab:gitlab.com/grp/proj@v1.2/starter")
	if err != nil || src.Scheme != "gitlab" || src.Host != "gitlab.com" || src.Repo != "grp/proj" || ref != "v1.2" || sub != "starter" {
		t.Errorf("gitlab parse = %+v %q %q err=%v", src, ref, sub, err)
	}
	if _, _, _, err := ParseSourceString("local"); err == nil {
		t.Error("local source must be rejected as remote")
	}
}

func TestFormatSourceRoundTrip(t *testing.T) {
	for _, s := range []string{
		"github.com/Templetry/web@main/react-spa",
		"gitlab:gitlab.com/grp/proj@main/starter",
		"gitea:codeberg.org/o/r@main/",
	} {
		src, ref, sub, err := ParseSourceString(s)
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		out := FormatSource(src, ref, sub)
		src2, ref2, sub2, err := ParseSourceString(out)
		if err != nil || src2 != src || ref2 != ref || sub2 != sub {
			t.Errorf("round trip %q -> %q lost data (%+v %q %q, err=%v)", s, out, src2, ref2, sub2, err)
		}
	}
}

func TestTarballURLs(t *testing.T) {
	cases := map[string]string{
		"Templetry/web":                  "https://codeload.github.com/Templetry/web/tar.gz/main",
		"gitlab:gitlab.com/grp/sub/proj": "https://gitlab.com/api/v4/projects/grp%2Fsub%2Fproj/repository/archive.tar.gz?sha=main",
		"gitea:codeberg.org/o/r":         "https://codeberg.org/api/v1/repos/o/r/archive/main.tar.gz",
	}
	for in, want := range cases {
		src, err := ParseRef(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := src.tarballURL("main"); got != want {
			t.Errorf("%s tarball:\n got %s\nwant %s", in, got, want)
		}
	}
}

func TestAuthHeaders(t *testing.T) {
	cases := map[string][2]string{
		"Templetry/web":          {"Authorization", "Bearer t"},
		"gitlab:gitlab.com/a/b":  {"PRIVATE-TOKEN", "t"},
		"gitea:codeberg.org/a/b": {"Authorization", "token t"},
	}
	for in, want := range cases {
		src, _ := ParseRef(in)
		k, v := src.authHeader("t")
		if k != want[0] || v != want[1] {
			t.Errorf("%s auth = %q %q, want %q %q", in, k, v, want[0], want[1])
		}
	}
	src, _ := ParseRef("Templetry/web")
	if k, _ := src.authHeader(""); k != "" {
		t.Error("empty token must produce no header")
	}
}
