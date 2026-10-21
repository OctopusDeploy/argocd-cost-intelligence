#!/usr/bin/env bash
# Checks that every copy of the trigger and template outside the chart carries exactly what the
# chart's install hook writes (helm/files/argocd/): the manual patch in deploy/ and the Helm values
# and Kustomize patch in docs/gitops-install.md, so the manual, GitOps and Helm installs can't drift.
# Exit 1 if any copy differs or is missing.
set -euo pipefail

cd "$(dirname "$0")/.."

FILES=helm/files/argocd
KEYS=(trigger.cost-intel-on-sync-succeeded template.cost-intel-sync-succeeded)
# Each file and how many copies of each key it holds.
COPIES=(deploy/argocd-notifications-patch.yaml:1 docs/gitops-install.md:2)

# Prints the literal block under the Nth `<key>: |` line of a file, with its indentation removed.
block() {
  awk -v key="$2: |" -v want="$3" '
    found {
      if ($0 == "") { print ""; next }
      match($0, /^ */)
      if (RLENGTH <= ind) exit
      if (!cind) cind = RLENGTH
      print substr($0, cind + 1)
      next
    }
    { line = $0; sub(/^ */, "", line) }
    line == key && ++n == want { found = 1; match($0, /^ */); ind = RLENGTH }
    END { exit found ? 0 : 3 }
  ' "$1"
}

status=0
for copy in "${COPIES[@]}"; do
  file=${copy%:*}
  for key in "${KEYS[@]}"; do
    for ((i = 1; i <= ${copy##*:}; i++)); do
      if ! got=$(block "$file" "$key" "$i"); then
        echo "$file has no copy $i of $key" >&2
        status=1
        continue
      fi
      if ! diff -u "$FILES/$key" <(printf '%s\n' "$got") >&2; then
        echo "copy $i of $key in $file differs from $FILES/$key (diff above)." >&2
        status=1
      fi
    done
  done
done
exit "$status"
