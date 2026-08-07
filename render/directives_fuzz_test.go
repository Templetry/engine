package render

import (
	"strings"
	"testing"
)

// FuzzFilterDirectives asserts the scanner never panics and that files
// without directives pass through byte-identical, whatever the input.
func FuzzFilterDirectives(f *testing.F) {
	f.Add("main.kt", "// tpl:if room\nx\n// tpl:endif\n")
	f.Add("a.py", "# tpl:var min_sdk 24\nminSdk = 24\n")
	f.Add("b.yml", "# tpl:if !web\nk: v\n# tpl:endif")
	f.Add("c.sql", "-- tpl:endif\n")
	f.Add("d.html", "<!-- tpl:if x --><p></p><!-- tpl:endif -->")
	f.Add("weird.kt", "// tpl:if a\n// tpl:if b\nnested\n// tpl:endif")
	f.Add("no-style.xyz", "tpl:if whatever\x00\xff")
	f.Fuzz(func(t *testing.T, path, content string) {
		features := map[string]bool{"room": true, "web": false, "a": true, "b": false, "x": true}
		vars := map[string]string{"min_sdk": "26"}
		out, err := FilterDirectives(path, []byte(content), features, vars)
		if err != nil {
			return // positioned errors are valid outcomes; panics are not
		}
		if !strings.Contains(content, "tpl:") && string(out) != content {
			t.Errorf("no directives in %q but output differs", path)
		}
	})
}
