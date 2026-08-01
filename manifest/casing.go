package manifest

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Words splits an identifier or phrase into lowercase words. It breaks on
// non-alphanumeric runes and on camel-case boundaries, keeping acronym runs
// together ("HTTPServer" -> ["http", "server"]).
func Words(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r):
			if i > 0 {
				prev := runes[i-1]
				nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextIsLower) {
					flush()
				}
			}
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}

func title(w string) string {
	r := []rune(w)
	if len(r) == 0 {
		return w
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// Casing renders value in the named casing. The empty casing returns the
// value untouched. Known casings: pascal, camel, kebab, snake, flat.
func Casing(value, casing string) (string, error) {
	if casing == "" {
		return value, nil
	}
	ws := Words(value)
	switch casing {
	case "pascal":
		for i, w := range ws {
			ws[i] = title(w)
		}
		return strings.Join(ws, ""), nil
	case "camel":
		for i, w := range ws {
			if i > 0 {
				ws[i] = title(w)
			}
		}
		return strings.Join(ws, ""), nil
	case "kebab":
		return strings.Join(ws, "-"), nil
	case "snake":
		return strings.Join(ws, "_"), nil
	case "flat":
		return strings.Join(ws, ""), nil
	default:
		return "", fmt.Errorf("unknown casing %q", casing)
	}
}

var placeholderRe = regexp.MustCompile(`\{([a-z][a-z0-9_]*)(?:\.([a-z]+))?\}`)

// Expand replaces {key} and {key.casing} placeholders in s using vars.
func Expand(s string, vars map[string]string) (string, error) {
	var firstErr error
	out := placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := placeholderRe.FindStringSubmatch(m)
		val, ok := vars[sub[1]]
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("unknown variable %q in %q", sub[1], s)
			}
			return m
		}
		cased, err := Casing(val, sub[2])
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%w in %q", err, s)
			}
			return m
		}
		return cased
	})
	return out, firstErr
}
