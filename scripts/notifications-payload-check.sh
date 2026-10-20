#!/usr/bin/env bash
# Checks that deploy/argocd-notifications-patch.yaml carries exactly the trigger and template the
# chart's install hook writes (helm/files/argocd/), so the manual and Helm installs can't drift.
# Exit 1 if either value differs.
set -euo pipefail

cd "$(dirname "$0")/.."

PATCH=deploy/argocd-notifications-patch.yaml
FILES=helm/files/argocd
KEYS=(trigger.cost-intel-on-sync-succeeded template.cost-intel-sync-succeeded)

# Prints the literal block under `data.<key>: |` with its 2+2 indentation removed.
block() {
  awk -v key="  $1: |" '
    found && ($0 == "" || /^    /) { print substr($0, 5); next }
    found { exit }
    $0 == key { found = 1; seen = 1 }
    END { exit seen ? 0 : 3 }
  ' "$PATCH"
}

status=0
for key in "${KEYS[@]}"; do
  if ! got=$(block "$key"); then
    echo "$PATCH has no $key" >&2
    status=1
    continue
  fi
  if ! diff -u "$FILES/$key" <(printf '%s\n' "$got") >&2; then
    echo "$PATCH differs from $FILES/$key (diff above)." >&2
    status=1
  fi
done
exit "$status"
