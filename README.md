# Templetry Engine

The core of [Templetry](https://github.com/Templetry): a pure Go library + thin CLI that renders ready-to-work repositories from compilable templates.

**Status: 🚀 Shipped — [v0.2.2](https://github.com/Templetry/engine/releases/latest).** Manifest parsing + validation + casings, planner (feature exclusion, identity renaming), renderer (directive scanner, JSON patches, deterministic answers file with drift anchor), remote catalog fetching (`list`/`init` against the official registry), `verify` in Docker and binary releases for linux/darwin/windows. Road to 1.0: [study VI](https://github.com/Templetry/wiki/blob/main/study/road-to-v1.md).

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
| `cmd/templetry` | Thin CLI: `list`, `init`, `plan`, `render`, `verify`, `version` |

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
```

## Development

```sh
go build ./...
go test ./...
```
