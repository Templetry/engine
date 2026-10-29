#!/bin/sh
# Package a templetry-mcp binary as an MCPB bundle (a zip holding manifest.json
# and the executable), the format the official MCP registry can point at from a
# GitHub release.
#
# usage: scripts/build-mcpb.sh <tag> <goos> <goarch> <binary> <out.mcpb>
set -eu

tag=$1 goos=$2 goarch=$3 binary=$4 out=$5
version=${tag#v}

case "$goos" in
  windows) platform=win32; exe=templetry-mcp.exe ;;
  darwin) platform=darwin; exe=templetry-mcp ;;
  *) platform=$goos; exe=templetry-mcp ;;
esac

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/server"
cp "$binary" "$work/server/$exe"
chmod 755 "$work/server/$exe"

cat > "$work/manifest.json" <<JSON
{
  "manifest_version": "0.3",
  "name": "templetry-mcp",
  "display_name": "Templetry",
  "version": "$version",
  "description": "Scaffold, plan, update and extend projects from Templetry templates.",
  "author": { "name": "Templetry", "url": "https://github.com/Templetry" },
  "repository": { "type": "git", "url": "https://github.com/Templetry/engine" },
  "license": "Apache-2.0",
  "server": {
    "type": "binary",
    "entry_point": "server/$exe",
    "mcp_config": {
      "command": "\${__dirname}/server/$exe",
      "args": []
    }
  },
  "compatibility": { "platforms": ["$platform"] }
}
JSON

out_abs=$(cd "$(dirname "$out")" && pwd)/$(basename "$out")
(cd "$work" && zip -q -r -X "$out_abs" manifest.json server)
