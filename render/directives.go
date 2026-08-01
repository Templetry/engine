package render

import (
	"fmt"
	pathpkg "path"
	"strings"
)

// style describes how comments look in one file family. Either line is set,
// or open/close are.
type style struct {
	line        string
	open, close string
}

var extStyles = map[string]style{
	".go": {line: "//"}, ".kt": {line: "//"}, ".kts": {line: "//"}, ".gradle": {line: "//"},
	".java": {line: "//"}, ".js": {line: "//"}, ".jsx": {line: "//"}, ".ts": {line: "//"},
	".tsx": {line: "//"}, ".swift": {line: "//"}, ".dart": {line: "//"}, ".rs": {line: "//"},
	".c": {line: "//"}, ".h": {line: "//"}, ".cpp": {line: "//"}, ".cs": {line: "//"},
	".scala": {line: "//"}, ".groovy": {line: "//"},
	".py": {line: "#"}, ".rb": {line: "#"}, ".sh": {line: "#"}, ".bash": {line: "#"},
	".yml": {line: "#"}, ".yaml": {line: "#"}, ".toml": {line: "#"}, ".properties": {line: "#"},
	".tf": {line: "#"}, ".gitignore": {line: "#"}, ".gitattributes": {line: "#"}, ".editorconfig": {line: "#"},
	".sql": {line: "--"}, ".lua": {line: "--"},
	".html": {open: "<!--", close: "-->"}, ".xml": {open: "<!--", close: "-->"},
	".md": {open: "<!--", close: "-->"}, ".vue": {open: "<!--", close: "-->"},
	".svg": {open: "<!--", close: "-->"},
}

var basenameStyles = map[string]style{
	"dockerfile": {line: "#"},
	"makefile":   {line: "#"},
}

func styleFor(path string) (style, bool) {
	if s, ok := basenameStyles[strings.ToLower(pathpkg.Base(path))]; ok {
		return s, true
	}
	s, ok := extStyles[strings.ToLower(pathpkg.Ext(path))]
	return s, ok
}

const prefix = "tpl:"

// extractDirective looks for a "tpl:" directive inside the line's comment.
// code is everything outside the directive comment; only is true when the
// line holds nothing but the directive.
func extractDirective(line string, st style) (code, directive string, only, found bool) {
	if st.line != "" {
		idx := strings.Index(line, st.line)
		if idx < 0 {
			return "", "", false, false
		}
		comment := strings.TrimSpace(line[idx+len(st.line):])
		if !strings.HasPrefix(comment, prefix) {
			return "", "", false, false
		}
		code = line[:idx]
		return code, strings.TrimSpace(comment[len(prefix):]), strings.TrimSpace(code) == "", true
	}
	open := strings.Index(line, st.open)
	if open < 0 {
		return "", "", false, false
	}
	rest := line[open+len(st.open):]
	closeIdx := strings.Index(rest, st.close)
	if closeIdx < 0 {
		return "", "", false, false
	}
	comment := strings.TrimSpace(rest[:closeIdx])
	if !strings.HasPrefix(comment, prefix) {
		return "", "", false, false
	}
	code = line[:open] + rest[closeIdx+len(st.close):]
	return code, strings.TrimSpace(comment[len(prefix):]), strings.TrimSpace(code) == "", true
}

// FilterDirectives runs the directive pass over one text file, per the
// normative spec (wiki spec/directives.md). Files whose comment style is
// unknown pass through untouched. All errors carry file:line.
func FilterDirectives(path string, data []byte, features map[string]bool, vars map[string]string) ([]byte, error) {
	st, known := styleFor(path)
	if !known {
		return data, nil
	}
	errAt := func(n int, format string, a ...any) error {
		return fmt.Errorf("%s:%d: %s", path, n+1, fmt.Sprintf(format, a...))
	}

	lines := strings.Split(string(data), "\n")
	type frame struct {
		line int
		keep bool
	}
	var stack []frame
	keeping := func() bool {
		for _, f := range stack {
			if !f.keep {
				return false
			}
		}
		return true
	}

	out := make([]string, 0, len(lines))
	for i, line := range lines {
		code, dir, only, found := extractDirective(line, st)
		if !found {
			if keeping() {
				out = append(out, line)
			}
			continue
		}
		fields := strings.Fields(dir)
		if len(fields) == 0 {
			return nil, errAt(i, "empty tpl: directive")
		}
		switch fields[0] {
		case "if":
			if !only {
				return nil, errAt(i, "tpl:if must be alone on its line")
			}
			if len(fields) != 2 {
				return nil, errAt(i, "tpl:if needs exactly one feature key")
			}
			key, negated := strings.CutPrefix(fields[1], "!")
			state, ok := features[key]
			if !ok {
				return nil, errAt(i, "unknown feature %q", key)
			}
			if negated {
				state = !state
			}
			stack = append(stack, frame{line: i, keep: state})
		case "endif":
			if !only {
				return nil, errAt(i, "tpl:endif must be alone on its line")
			}
			if len(stack) == 0 {
				return nil, errAt(i, "tpl:endif without an open tpl:if")
			}
			stack = stack[:len(stack)-1]
		case "var":
			if len(fields) < 3 {
				return nil, errAt(i, "tpl:var needs <key> <literal>")
			}
			if !keeping() {
				continue
			}
			val, ok := vars[fields[1]]
			if !ok {
				return nil, errAt(i, "unknown variable %q", fields[1])
			}
			literal := strings.Join(fields[2:], " ")
			idx := strings.Index(code, literal)
			if idx < 0 {
				return nil, errAt(i, "tpl:var literal %q not found on line", literal)
			}
			out = append(out, strings.TrimRight(code[:idx]+val+code[idx+len(literal):], " \t"))
		default:
			return nil, errAt(i, "unknown directive tpl:%s", fields[0])
		}
	}
	if len(stack) > 0 {
		return nil, errAt(stack[len(stack)-1].line, "tpl:if never closed")
	}
	return []byte(strings.Join(out, "\n")), nil
}
