# Templetry Engine

The core of [Templetry](https://github.com/Templetry): a pure Go library + thin CLI that renders ready-to-work repositories from compilable templates.

**Status: 🚀 [v1.7.0](https://github.com/Templetry/engine/releases/latest)** — the manifest, directives, answers-file and registry schemas plus the CLI surface are stable public API under the [compatibility policy](https://github.com/Templetry/wiki/blob/main/spec/compatibility.md) ([ADR-0013](https://github.com/Templetry/wiki/blob/main/adr/0013-declare-v1.md)).

Since v1.0: `templetry update` (three-way merge), `templetry verify` in Docker, **lazy pieces** (`pieces`/`add`), YAML/TOML patches, feature `requires`/`conflicts` and `presets`, **multi-forge template hosting** (`github:`, `gitlab:`, `gitea:` source schemes) and the `templetry-mcp` server. Output paths are hardened against escapes and platform quirks; the directive scanner is fuzz-tested.

## Principles (from the [wiki](https://github.com/Templetry/wiki))

- **The engine doesn't know what a framework is** — all knowledge lives in each template's `template.yml` ([ADR-0002](https://github.com/Templetry/wiki/blob/main/adr/0002-knowledge-lives-in-the-manifest.md)).
- **Templates compile** — three transformation mechanisms: identity-map renaming, comment directives, structured patches ([ADR-0003](https://github.com/Templetry/wiki/blob/main/adr/0003-templates-compile.md)).
- **Pure core, effectful edges** — deterministic: same inputs, byte-identical output.

## Architecture

| Package | Responsibility |
|---|---|
| `manifest` | Parse + validate `template.yml`, derive casings |
| `source` | Obtain the template as an in-memory FileSet |
| `planner` | (manifest, inputs, FileSet) → Plan — pure |
| `ops` | Operation types as data |
| `render` | Execute a Plan against a virtual file tree |
| `verify` | Run the template's verify command in Docker |
| `update` | The update cycle: re-render with recorded inputs, diff, three-way merge |
| `answers` | The `.templetry-answers.yml` record: one deterministic reader/emitter |
| `piece` | Lazy pieces (ADR-0014): decoupled units with their own lifecycle |
| `cmd/templetry` | Thin CLI: `list`, `init`, `plan`, `render`, `verify`, `update`, `pieces`, `add`, `version` |
| `cmd/templetry-mcp` | MCP server (stdio): the engine's verbs as tools for AI agents |

Design rationale: [study I](https://github.com/Templetry/wiki/blob/main/study/engine-v1.md) · [study II](https://github.com/Templetry/wiki/blob/main/study/engine-tech-v1.md).

## Install

Three ways, pick one:

1. **Binary** — download your platform's executable from [Releases](https://github.com/Templetry/engine/releases), rename it to `templetry` (or `templetry.exe`) and put it on your `PATH`.
2. **Go toolchain** — `go install github.com/Templetry/engine/cmd/templetry@latest` (installs to `$(go env GOPATH)/bin`).
3. **From source** — clone and `go build -o templetry ./cmd/templetry`.

Then:

```sh
templetry list                                  # browse the official catalog
templetry init kmp/single-module --out my-app \
  --set "project_name=My App" --set "base_package=com.me.myapp"
templetry plan --template <dir> --set key=value --feature name
templetry render --template <dir> --out <dir> --set key=value
templetry verify --template <dir> --set key=value    # render to a temp dir and compile it in Docker
templetry update ./my-app                            # preview a template update (--apply to write)
templetry pieces ./my-app                            # lazy pieces the template ships
templetry add axios-api ./my-app --set api_base=/v2  # adopt a piece into a living project
```

## MCP server (AI agents)

`templetry-mcp` (in the same releases) speaks the Model Context Protocol over stdio — no dependencies, no configuration. It exposes five tools: `list_templates`, `get_form_schema`, `plan`, `render` and `update`, so an agent can browse the catalog, scaffold a ready-to-work project and keep it updated. Example Claude Code registration:

```sh
claude mcp add templetry -- templetry-mcp
```

## Development

```sh
go build ./...
go test ./...
```

## License

[PolyForm Noncommercial 1.0.0](LICENSE.md) — free for any noncommercial purpose; commercial use requires the author's permission. The templates and the desktop app live in their own repos under MIT.
