# Publishing `templetry-mcp` to the MCP registry

`templetry-mcp` is not yet listed in the [official MCP registry](https://registry.modelcontextprotocol.io).
Everything needed is in the repository; publishing stays off until the owner
switches it on.

## How the registry sees us

The registry only installs from package registries it trusts: npm, PyPI,
NuGet, crates.io, OCI images (Docker Hub, GHCR, ...) and **MCPB** bundles hosted
on GitHub or GitLab releases. `templetry-mcp` is a static Go binary shipped as
release assets, so the smallest route that publishes nothing elsewhere is MCPB:

- `scripts/build-mcpb.sh` wraps each `templetry-mcp-<os>-<arch>` binary in an
  `.mcpb` bundle (a zip with `manifest.json` and the executable). The Release
  workflow builds six of them next to the plain binaries and covers them in
  `SHA256SUMS`.
- [`server.json`](../server.json) lists the six bundles as `mcpb` packages
  (stdio transport) under the name `io.github.Templetry/templetry-mcp`. The
  committed `version` and `fileSha256` values are placeholders; the publish
  workflow replaces them from the release before publishing. Never publish the
  file as committed.

The alternatives, if MCPB is not wanted: an OCI image on GHCR (a `Dockerfile`
plus a push step) or an npm wrapper package that downloads the binary. Both mean
publishing to a second place, which MCPB avoids.

## Enabling publishing

1. Make sure the GitHub organization `Templetry` is what GitHub OIDC reports as
   the repository owner (it is); the registry then allows the namespace
   `io.github.Templetry/*` with no secrets.
2. In the engine repository: Settings > Secrets and variables > Actions >
   Variables > New repository variable `MCP_PUBLISH` = `true`.
3. Cut a release as usual (push a `v*` tag). When the Release workflow
   succeeds, [`publish-mcp.yml`](../.github/workflows/publish-mcp.yml) fills
   `server.json` from the release's `SHA256SUMS`, logs in with
   `mcp-publisher login github-oidc` and runs `mcp-publisher publish`.
4. To publish an existing release (the first one needs the new bundles, so it
   must be a release made after this change): Actions > Publish MCP > Run
   workflow, with the tag.

Set the variable back to anything else to turn it off again.

## Checking by hand

```sh
mcp-publisher login github      # device flow, as yourself
mcp-publisher publish           # after editing server.json for a real release
```

The registry limits `description` to 100 characters and does not verify
`fileSha256`; MCP clients do, before installing.
