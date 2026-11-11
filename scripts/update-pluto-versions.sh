#!/usr/bin/env bash
# Replace knowledge/pluto/versions.yaml with one Pluto release and record that
# release in knowledge/pluto/source.yaml.
#
# Pluto releases: https://github.com/FairwindsOps/pluto/releases
# The versions file for a tag lives at:
#   https://github.com/FairwindsOps/pluto/blob/<tag>/versions.yaml
#
# Usage:
#   scripts/update-pluto-versions.sh v5.24.5
#   make update-pluto VERSION=v5.24.5
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
version=${1:-}

if [[ -z "$version" || "$version" == "-h" || "$version" == "--help" ]]; then
	echo "usage: scripts/update-pluto-versions.sh <pluto-tag>" >&2
	echo "releases: https://github.com/FairwindsOps/pluto/releases" >&2
	echo "example: scripts/update-pluto-versions.sh v5.24.5" >&2
	exit 2
fi
if [[ "$version" != v* ]]; then
	version="v${version}"
fi

raw="https://raw.githubusercontent.com/FairwindsOps/pluto/${version}/versions.yaml"
dest="$root/knowledge/pluto/versions.yaml"
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

curl -fsSL -o "$tmp" "$raw"
grep -q "deprecated-versions:" "$tmp"
grep -q "component: k8s" "$tmp"
sum=$(shasum -a 256 "$tmp" | awk '{print $1}')

mv "$tmp" "$dest"
trap - EXIT

cat >"$root/knowledge/pluto/source.yaml" <<EOF
module: github.com/fairwindsops/pluto/v5
version: ${version}
url: https://github.com/FairwindsOps/pluto/blob/${version}/versions.yaml
sha256: ${sum}
EOF

echo "pinned Pluto ${version}"
echo "sha256 ${sum}"
echo "source ${dest}"
echo "run go test ./... before committing"
