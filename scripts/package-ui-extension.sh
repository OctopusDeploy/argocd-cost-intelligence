#!/usr/bin/env bash
# Packages the built UI plugin for the Argo CD extension installer
# (https://github.com/argoproj-labs/argocd-extension-installer), which the release attaches.
#
#   extension.tar.gz          resources/extension-cost-intel.js; the installer copies resources/*
#                             into Argo CD's extensions directory, which loads files named extension*.js
#   extension_checksums.txt   "<sha256>  extension.tar.gz"; the installer looks the archive up by name
#
# Usage: scripts/package-ui-extension.sh [OUT_DIR]   (default dist/). Run `npm run build` in ui/ first.
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
bundle="$repo/ui/dist/index.js"
out=${1:-"$repo/dist"}

if [ ! -s "$bundle" ]; then
  echo "error: $bundle is missing or empty; run 'npm run build' in ui/ first" >&2
  exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/resources" "$out"
cp "$bundle" "$work/resources/extension-cost-intel.js"

# COPYFILE_DISABLE keeps macOS tar from adding ._ metadata files.
COPYFILE_DISABLE=1 tar -czf "$out/extension.tar.gz" -C "$work" resources

cd "$out"
if command -v sha256sum > /dev/null; then
  sha256sum extension.tar.gz > extension_checksums.txt
else
  shasum -a 256 extension.tar.gz > extension_checksums.txt
fi

echo "Wrote $out/extension.tar.gz and $out/extension_checksums.txt"
