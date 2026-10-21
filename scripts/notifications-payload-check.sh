#!/usr/bin/env bash
# Checks that the copies of what the chart's install hook writes to Argo CD carry exactly what the
# hook writes: the webhook service, trigger and template in the manual patch in deploy/, and those
# plus the subscription and RBAC policy in the Helm values and Kustomize patch in
# docs/gitops-install.md, so the manual, GitOps and Helm installs can't drift. Examples in comments
# and prose aren't read. It also checks that nothing here creates Kubernetes RBAC under one of
# Argo CD's own names.
# The expected values come from rendering the hook with the chart's defaults, which is the install
# the copies describe. Needs helm and jq. Exit 1 if any copy differs or is missing.
set -euo pipefail

cd "$(dirname "$0")/.."

# Each copy as file:key:count, the number of times the key appears in that file.
COPIES=(
  deploy/argocd-notifications-patch.yaml:service.webhook.cost-intel:1
  deploy/argocd-notifications-patch.yaml:trigger.cost-intel-on-sync-succeeded:1
  deploy/argocd-notifications-patch.yaml:template.cost-intel-sync-succeeded:1
  docs/gitops-install.md:service.webhook.cost-intel:2
  docs/gitops-install.md:trigger.cost-intel-on-sync-succeeded:2
  docs/gitops-install.md:template.cost-intel-sync-succeeded:2
  docs/gitops-install.md:subscriptions:2
  docs/gitops-install.md:policy.cost-intel.csv:2
)

hook=$(helm template argocd-cost-intelligence ./helm --show-only templates/post-install-hook.yaml \
  --set postgres.auth.password=check)

# Prints the value of the hook's environment variable $1, decoded.
env_value() {
  awk -v name="- name: $1" '
    found { sub(/^ *value: /, ""); print; exit }
    { line = $0; sub(/^ */, "", line) }
    line == name { found = 1 }
  ' <<< "$hook" | jq -r .
}

patch=$(env_value NOTIFICATIONS_PATCH)
policy=$(env_value RBAC_POLICY)
entry=$(sed -n "s/^ *ENTRY='\(.*\)'$/\1/p" <<< "$hook")
if [ -z "$patch" ] || [ -z "$policy" ] || [ -z "$entry" ]; then
  echo "couldn't find the notifications patch, RBAC policy or subscription entry in the rendered hook" >&2
  exit 1
fi

# A notifications key the hook writes must have a copy listed above.
for key in $(jq -r '.data | keys[]' <<< "$patch"); do
  if [[ " ${COPIES[*]} " != *":$key:"* ]]; then
    echo "the hook writes $key to argocd-notifications-cm, but no copy of it is checked; add its copies to COPIES" >&2
    exit 1
  fi
done

# Prints what the hook writes for a key.
expected() {
  case $1 in
    subscriptions)
      # The entry the hook adds, as one YAML list item of lists.
      jq -r 'to_entries | to_entries[]
        | (if .key == 0 then "- " else "  " end) + .value.key + ":", (.value.value[] | "  - " + .)' <<< "$entry" ;;
    policy.cost-intel.csv) printf '%s\n' "$policy" ;;
    *) jq -r --arg k "$1" '.data[$k] // error("the hook writes no \($k)")' <<< "$patch" ;;
  esac
}

# Prints the block under the Nth `<key>: |` or `<key>:` line of a file, with its indentation removed.
block() {
  awk -v key="$2:" -v want="$3" '
    found {
      if ($0 == "") { print ""; next }
      match($0, /^ */)
      if (RLENGTH <= ind) exit
      if (!cind) cind = RLENGTH
      print substr($0, cind + 1)
      next
    }
    { line = $0; sub(/^ */, "", line) }
    (line == key || line == key " |") && ++n == want { found = 1; match($0, /^ */); ind = RLENGTH }
    END { exit found ? 0 : 3 }
  ' "$1"
}

status=0
for copy in "${COPIES[@]}"; do
  IFS=: read -r file key count <<< "$copy"
  want=$(expected "$key")
  for ((i = 1; i <= count; i++)); do
    if ! got=$(block "$file" "$key" "$i"); then
      echo "$file has no copy $i of $key" >&2
      status=1
      continue
    fi
    if ! diff -u --label "what the hook writes" --label "$file, copy $i" \
      <(printf '%s\n' "$want") <(printf '%s\n' "$got") >&2; then
      echo "copy $i of $key in $file differs from what the hook writes (diff above)." >&2
      status=1
    fi
  done
done

# Kubernetes RBAC named argocd-* is Argo CD's own: its Helm chart has a ClusterRole and binding
# named argocd-notifications-controller, and an apply under the same name replaces its rules. So no
# Role, ClusterRole or binding with such a name, other than this chart's argocd-cost-intelligence*,
# may come from the rendered chart (including YAML in the hook's script), the hook's kubectl commands,
# deploy/, the READMEs or the docs.
rbac_names() {
  awk '
    function emit() { if (kind ~ /^(Cluster)?Role(Binding)?$/ && name != "") print file ": " kind " " name; kind = ""; name = ""; meta = 0 }
    function indent() { match($0, /^[ -]*/); return RLENGTH }
    function value() { v = $0; sub(/^[^:]*: */, "", v); sub(/ *#.*$/, "", v); gsub(/["\047]/, "", v); return v }
    FNR == 1 { emit() }
    /^ *---/ || /^ *```/ { emit(); next }
    # A kind: at any depth, also as a list item, starts the next object.
    /^[ -]*kind: / { if (kind != "") emit(); kind = value(); file = FILENAME }
    /^[ -]*metadata:/ { meta = 1; mind = indent(); cind = 0; next }
    meta && /[^ ]/ && indent() <= mind { meta = 0 }
    meta && /[^ ]/ && !cind { cind = indent() }
    # Only metadata'"'"'s own name, not one in its labels.
    meta && name == "" && /^ *name: / && indent() == cind { name = value() }
    match($0, /kubectl create (clusterrole|clusterrolebinding|role|rolebinding) [^ ]+/) {
      split(substr($0, RSTART, RLENGTH), w, " "); print FILENAME ": " w[3] " " w[4]
    }
    END { emit() }
  ' "$@"
}
chart=$(helm template argocd-cost-intelligence ./helm --set postgres.auth.password=check)
# Kept apart from the filter below, so that if awk fails the script stops instead of finding nothing.
names=$(rbac_names <(printf '%s\n' "$chart") deploy/*.yaml docs/*.md README.md helm/README.md)
if taken=$(grep -E ' argocd-' <<< "$names" | grep -vE ' argocd-cost-intelligence' \
  | sed 's|^/dev/fd/[0-9]*:|rendered chart:|'); then
  printf '%s\n' "$taken" >&2
  echo "RBAC named after Argo CD's own objects (above); give it a Cost Intelligence name, or leave it to Argo CD's install." >&2
  status=1
fi
exit "$status"
