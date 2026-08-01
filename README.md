# Templetry Engine

The core of [Templetry](https://github.com/Templetry): a pure Go library + thin CLI that renders ready-to-work repositories from compilable templates.

**Status: ⚙️ Phase 1 — core implemented.** Manifest parsing + validation + casings, planner (feature exclusion, identity renaming), renderer (directive scanner, JSON patches, deterministic answers file) and the `plan`/`render` CLI are working, with unit + golden tests. Next: remote template fetching and `verify` (Phase 2).

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
| `cmd/templetry` | Thin CLI: `plan`, `render`, `version` |

Design rationale: [study I](https://github.com/Templetry/wiki/blob/main/study/engine-v1.md) · [study II](https://github.com/Templetry/wiki/blob/main/study/engine-tech-v1.md).

## Development

```sh
go build ./...
go test ./...
```
