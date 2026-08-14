package piece

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Templetry/engine/answers"
	"github.com/Templetry/engine/manifest"
	"github.com/Templetry/engine/source"
)

func formManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load([]byte(`
schema_version: 1
name: demo
variables:
  - key: project_name
    type: string
identity:
  - from: "template-app"
    to: "{project_name.kebab}"
`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func pieceFiles(t *testing.T, yml string, files map[string]string) (*Manifest, *source.FileSet) {
	t.Helper()
	pm, err := Load([]byte(yml))
	if err != nil {
		t.Fatal(err)
	}
	fs := source.NewFileSet()
	fs.Put("piece.yml", &source.File{Data: []byte(yml)})
	for p, data := range files {
		fs.Put(p, &source.File{Data: []byte(data)})
	}
	return pm, fs
}

const apiPiece = `
schema_version: 1
name: api
variables:
  - key: base_url
    default: /api
patches:
  - file: config.json
    op: add
    path: /api
    value: "{base_url}"
`

func TestApplyRendersRenamesAndPatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{\n  \"name\": \"x\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pm, fs := pieceFiles(t, apiPiece, map[string]string{
		"src/template-app-api.ts": "export const app = \"template-app\";\nexport const base = \"/api\"; // tpl:var base_url /api\n",
	})
	res, err := Apply(dir, formManifest(t), pm, fs, map[string]string{"project_name": "My App"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0] != "src/my-app-api.ts" {
		t.Errorf("files = %v", res.Files)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "src", "my-app-api.ts"))
	if !strings.Contains(string(data), `"my-app"`) {
		t.Errorf("identity not applied: %s", data)
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if !strings.Contains(string(cfg), `"api": "/api"`) {
		t.Errorf("patch not applied: %s", cfg)
	}
	if res.Variables["base_url"] != "/api" {
		t.Errorf("resolved vars = %v", res.Variables)
	}
}

func TestApplyRefusesCollisions(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "index.ts"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	pm, fs := pieceFiles(t, "schema_version: 1\nname: clash\n", map[string]string{
		"src/index.ts": "theirs",
	})
	_, err := Apply(dir, formManifest(t), pm, fs, map[string]string{"project_name": "My App"}, nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("collision must be refused, got %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "src", "index.ts"))
	if string(data) != "mine" {
		t.Errorf("project file was touched: %s", data)
	}
}

func TestApplyRefusesVariableClash(t *testing.T) {
	dir := t.TempDir()
	pm, fs := pieceFiles(t, "schema_version: 1\nname: p\nvariables:\n  - key: project_name\n    default: x\n", nil)
	_, err := Apply(dir, formManifest(t), pm, fs, map[string]string{"project_name": "My App"}, nil)
	if err == nil || !strings.Contains(err.Error(), "clashes") {
		t.Fatalf("variable clash must be refused, got %v", err)
	}
}

func TestListAndExtract(t *testing.T) {
	fs := source.NewFileSet()
	fs.Put("template.yml", &source.File{Data: []byte("x")})
	fs.Put("pieces/api/piece.yml", &source.File{Data: []byte("schema_version: 1\nname: api\ndescription: API client\n")})
	fs.Put("pieces/api/src/a.ts", &source.File{Data: []byte("x")})
	fs.Put("pieces/store/piece.yml", &source.File{Data: []byte("schema_version: 1\nname: store\n")})
	ans := answers.Answers{Pieces: []answers.AppliedPiece{{Name: "store"}}}
	infos := List(fs, ans)
	if len(infos) != 2 || infos[0].Name != "api" || infos[0].Applied || !infos[1].Applied {
		t.Errorf("list = %+v", infos)
	}
	sub, err := Extract(fs, "api")
	if err != nil || sub.Len() != 2 {
		t.Fatalf("extract: %v len=%d", err, sub.Len())
	}
	if _, err := Extract(fs, "ghost"); err == nil || !strings.Contains(err.Error(), "available: api, store") {
		t.Errorf("ghost extract error = %v", err)
	}
}

// A piece per object: the piece is written against a canonical entity name
// and renames it through its own identity map (ADR-0014).
const entityPiece = `
schema_version: 1
name: crud-resource
variables:
  - key: entity
    label: Entity name
identity:
  - from: "TemplateEntity"
    to: "{entity.pascal}"
  - from: "template_entity"
    to: "{entity.snake}"
`

func TestPieceIdentityRenamesEntity(t *testing.T) {
	dir := t.TempDir()
	pm, fs := pieceFiles(t, entityPiece, map[string]string{
		"src/template_entity.ts": "export class TemplateEntity {}\n// template-app owns this\n",
	})
	res, err := Apply(dir, formManifest(t), pm, fs,
		map[string]string{"project_name": "My App"}, map[string]string{"entity": "product order"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0] != "src/product_order.ts" {
		t.Fatalf("files = %v, want src/product_order.ts", res.Files)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "src", "product_order.ts"))
	got := string(data)
	if !strings.Contains(got, "class ProductOrder") {
		t.Errorf("entity not renamed in content: %s", got)
	}
	// The form's identity still applies alongside the piece's.
	if !strings.Contains(got, "my-app owns this") {
		t.Errorf("form identity lost: %s", got)
	}
}

func TestUnderscorePieceRoot(t *testing.T) {
	// _pieces/ keeps a form compiling in place under glob-based toolchains.
	fs := source.NewFileSet()
	fs.Put("template.yml", &source.File{Data: []byte("x")})
	fs.Put("_pieces/crud/piece.yml", &source.File{Data: []byte("schema_version: 1\nname: crud\ndescription: CRUD\n")})
	fs.Put("_pieces/crud/internal/store/x.go", &source.File{Data: []byte("package store\n")})
	infos := List(fs, answers.Answers{})
	if len(infos) != 1 || infos[0].Name != "crud" || infos[0].Description != "CRUD" {
		t.Fatalf("list = %+v", infos)
	}
	sub, err := Extract(fs, "crud")
	if err != nil || sub.Len() != 2 || sub.Get("internal/store/x.go") == nil {
		t.Fatalf("extract: %v len=%d", err, sub.Len())
	}
}
