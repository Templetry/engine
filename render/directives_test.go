package render

import (
	"strings"
	"testing"
)

func filter(t *testing.T, path, in string, features map[string]bool, vars map[string]string) string {
	t.Helper()
	out, err := FilterDirectives(path, []byte(in), features, vars)
	if err != nil {
		t.Fatalf("FilterDirectives(%s): %v", path, err)
	}
	return string(out)
}

func TestIfEndif(t *testing.T) {
	in := "a\n// tpl:if room\nkeep\n// tpl:endif\nb\n"
	if got := filter(t, "x.kt", in, map[string]bool{"room": true}, nil); got != "a\nkeep\nb\n" {
		t.Errorf("active: got %q", got)
	}
	if got := filter(t, "x.kt", in, map[string]bool{"room": false}, nil); got != "a\nb\n" {
		t.Errorf("inactive: got %q", got)
	}
}

func TestNegationAndNesting(t *testing.T) {
	in := "// tpl:if !room\nno-room\n// tpl:endif\n// tpl:if a\n// tpl:if b\nboth\n// tpl:endif\n// tpl:endif\n"
	got := filter(t, "x.go", in, map[string]bool{"room": false, "a": true, "b": false}, nil)
	if got != "no-room\n" {
		t.Errorf("got %q", got)
	}
}

func TestVar(t *testing.T) {
	in := "minSdk = 24 // tpl:var min_sdk 24\n"
	got := filter(t, "b.gradle.kts", in, nil, map[string]string{"min_sdk": "26"})
	if got != "minSdk = 26\n" {
		t.Errorf("got %q", got)
	}
}

func TestBlockComments(t *testing.T) {
	in := "<p>hi</p>\n<!-- tpl:if ads -->\n<script></script>\n<!-- tpl:endif -->\n"
	if got := filter(t, "index.html", in, map[string]bool{"ads": false}, nil); got != "<p>hi</p>\n" {
		t.Errorf("got %q", got)
	}
}

func TestUnknownExtensionPassesThrough(t *testing.T) {
	in := "{ \"x\": 1 }\n"
	if got := filter(t, "data.json", in, nil, nil); got != in {
		t.Errorf("json must not be scanned, got %q", got)
	}
}

func TestHashStyle(t *testing.T) {
	in := "# tpl:if pg\ndep = 1\n# tpl:endif\n"
	if got := filter(t, "pyproject.toml", in, map[string]bool{"pg": true}, nil); got != "dep = 1\n" {
		t.Errorf("got %q", got)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct{ name, path, in, wantErr string }{
		{"unknown feature", "x.kt", "// tpl:if nope\n// tpl:endif\n", `unknown feature "nope"`},
		{"unclosed", "x.kt", "// tpl:if a\n", "never closed"},
		{"stray endif", "x.kt", "// tpl:endif\n", "without an open"},
		{"code with if", "x.kt", "val x = 1 // tpl:if a\n// tpl:endif\n", "alone on its line"},
		{"unknown directive", "x.kt", "// tpl:unless a\n", "unknown directive"},
		{"var literal missing", "x.kt", "y = 9 // tpl:var v 24\n", "not found"},
		{"var unknown key", "x.kt", "y = 24 // tpl:var nope 24\n", `unknown variable "nope"`},
	}
	feats := map[string]bool{"a": true}
	vars := map[string]string{"v": "1"}
	for _, c := range cases {
		_, err := FilterDirectives(c.path, []byte(c.in), feats, vars)
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: got %v, want containing %q", c.name, err, c.wantErr)
		}
		if err != nil && !strings.Contains(err.Error(), c.path+":") {
			t.Errorf("%s: error must carry file:line, got %v", c.name, err)
		}
	}
}
