package render

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Templetry/engine/ops"
)

const answersPath = ".templetry-answers.yml"

var bareScalarRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 ._-]*$`)

func yamlScalar(s string) string {
	if bareScalarRe.MatchString(s) && !strings.HasSuffix(s, " ") {
		return s
	}
	return strconv.Quote(s)
}

// answersYAML emits the deterministic answers file (wiki spec/answers-file.md):
// sorted keys, no timestamp, byte-stable for identical inputs.
func answersYAML(p *ops.Plan) []byte {
	var b strings.Builder
	b.WriteString("schema_version: 1\n")
	b.WriteString("template:\n")
	fmt.Fprintf(&b, "  name: %s\n", p.Template)
	b.WriteString("  source: local\n")
	if len(p.Variables) > 0 {
		b.WriteString("variables:\n")
		for _, k := range sortedKeys(p.Variables) {
			fmt.Fprintf(&b, "  %s: %s\n", k, yamlScalar(p.Variables[k]))
		}
	}
	if len(p.Features) > 0 {
		b.WriteString("features:\n")
		keys := make([]string, 0, len(p.Features))
		for k := range p.Features {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: %t\n", k, p.Features[k])
		}
	}
	return []byte(b.String())
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
