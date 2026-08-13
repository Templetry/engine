package render

import (
	"github.com/Templetry/engine/answers"
	"github.com/Templetry/engine/ops"
)

const answersPath = answers.Path

// answersYAML emits the deterministic answers file for a plan through the
// shared emitter (wiki spec/answers-file.md).
func answersYAML(p *ops.Plan) []byte {
	a := answers.Answers{SchemaVersion: 1, Variables: p.Variables, Features: p.Features}
	a.Template.Name = p.Template
	a.Template.Source = p.Source
	a.Template.Commit = p.SourceCommit
	return answers.Marshal(a)
}
