package answers

import (
	"strings"
	"testing"
)

func TestRoundTripWithPieces(t *testing.T) {
	a := Answers{SchemaVersion: 1, Variables: map[string]string{"project_name": "Demo App"}}
	a.Template.Name = "web-react-spa"
	a.Template.Source = "github.com/Templetry/web@main/react-spa"
	a.Template.Commit = "abc123"
	a.Features = map[string]bool{"router": true}
	a.Pieces = []AppliedPiece{{
		Name: "api", Source: a.Template.Source + "/pieces/api", Commit: "def456",
		Variables: map[string]string{"base_url": "/api"},
		Files:     []string{"src/api/client.ts"},
	}}
	dir := t.TempDir()
	if err := Write(dir, a); err != nil {
		t.Fatal(err)
	}
	back, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if back.Template.Commit != "abc123" || len(back.Pieces) != 1 ||
		back.Pieces[0].Variables["base_url"] != "/api" || back.Pieces[0].Files[0] != "src/api/client.ts" {
		t.Errorf("round trip lost data: %+v", back)
	}
	// Deterministic re-emit.
	if string(Marshal(a)) != string(Marshal(back)) {
		t.Errorf("marshal not stable:\n%s\nvs\n%s", Marshal(a), Marshal(back))
	}
	if !strings.Contains(string(Marshal(a)), "pieces:") {
		t.Errorf("pieces section missing")
	}
}
