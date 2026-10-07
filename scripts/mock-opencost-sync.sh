#!/usr/bin/env bash
# Keeps the copy of mock-opencost-server.js embedded in deploy/mock-opencost.yaml in step with the
# script itself.
#   mock-opencost-sync.sh check   exit 1 if the manifest's copy differs
#   mock-opencost-sync.sh write   regenerate the manifest from the script
set -euo pipefail

cd "$(dirname "$0")/.."

SRC=mock-opencost-server.js
MANIFEST=deploy/mock-opencost.yaml
START='  mock-opencost-server.js: |'

render() {
  awk -v start="$START" -v src="$SRC" '
    state == 0 { print }
    state == 0 && $0 == start {
      state = 1
      while ((getline line < src) > 0) print (line == "" ? "" : "    " line)
      next
    }
    state == 1 && /^---$/ { state = 2; print; next }
    state == 2 { print }
  ' "$MANIFEST"
}

case "${1:-check}" in
  check)
    if ! diff -u "$MANIFEST" <(render) >/dev/null; then
      echo "$MANIFEST is out of step with $SRC. Run: make sync-mock" >&2
      exit 1
    fi
    ;;
  write)
    tmp=$(mktemp)
    render >"$tmp"
    mv "$tmp" "$MANIFEST"
    ;;
  *)
    echo "usage: $0 [check|write]" >&2
    exit 2
    ;;
esac
