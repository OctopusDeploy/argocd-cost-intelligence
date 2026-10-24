# Install when Git manages Argo CD

This guide is for you if Argo CD itself is installed and configured from Git, for example with Argo CD managing its own Helm release or a Kustomize overlay. In that case the chart's install hook is the wrong tool. It changes Argo CD's ConfigMaps and the `argocd-server` Deployment with `kubectl patch`, and those changes live only in the cluster. They are lost when your Git sets the same key, and they don't come back when you rebuild Argo CD from Git. So you turn the hook off and put the same settings in your own Argo CD configuration.

You need the [prerequisites in Installation](installation.md#prerequisites) first: Argo CD 3.4, 3.5 or 3.6 with Argo CD Notifications, and OpenCost.

Steps 1 and 2 are the same for every setup. Step 3 has one section for [Argo CD's Helm chart](#argo-cds-helm-chart) and one for [plain manifests or Kustomize](#plain-manifests-or-kustomize); follow only yours.

## What you'll add

These are all the changes the hook makes to Argo CD. You'll add each of them to your Argo CD configuration.

| Object in the `argocd` namespace | Key or field | Why it's needed |
|---|---|---|
| `argocd-server` Deployment | an init container and a volume at `/tmp/extensions` | Puts the plugin's JavaScript where `argocd-server` loads UI extensions from |
| `argocd-cm` ConfigMap | `extension.config` | Tells Argo CD where to send the plugin's API calls |
| `argocd-cmd-params-cm` ConfigMap | `server.enable.proxy.extension` | Turns on proxy extensions, which are off by default |
| `argocd-rbac-cm` ConfigMap | `policy.cost-intel.csv` | Lets users open the plugin. On Argo CD 3.5 and later it is denied to everyone until a policy allows it |
| `argocd-notifications-cm` ConfigMap | `service.webhook.cost-intel`, `trigger.cost-intel-on-sync-succeeded`, `template.cost-intel-sync-succeeded`, `subscriptions` | Calls Cost Intelligence after each successful sync, which is when costs are recorded |
| `argocd-notifications-secret` Secret | `cost-intel-webhook-token` | The token the webhook sends, so Cost Intelligence accepts the call |

The webhook only goes out for Applications the notifications controller can read. If yours live outside the `argocd` namespace, it also needs Argo CD's own notifications ClusterRole, which each path below covers.

## Values to change

The examples below use these names. Change them everywhere if yours are different.

| Name in the examples | What it is |
|---|---|
| `argocd` | The namespace Argo CD runs in |
| `argocd-cost-intel` | The namespace Cost Intelligence runs in |
| `argocd-cost-intelligence` | The name of the Cost Intelligence Application. The chart names its Services after it, so it appears in the URLs below |
| `<chart-version>` | The Cost Intelligence chart version you install |

If you rename the Application to a name that doesn't contain `argocd-cost-intelligence`, the backend Service becomes `<name>-argocd-cost-intelligence` and the UI Service `<name>-argocd-cost-intelligence-ui`. A name that contains it is used as it is. Run `kubectl get svc -n argocd-cost-intel` after the first sync to see the real names.

## Step 1: Create the namespace and the secrets

Cost Intelligence needs a webhook token and a database password that stay the same every time Argo CD renders the chart. The chart can't generate them when Argo CD renders it, so you create them once.

Create the namespace and a token:

```bash
kubectl create namespace argocd-cost-intel
TOKEN=$(openssl rand -hex 24)
```

Store the token in two places. Cost Intelligence reads it from its own namespace, and Argo CD Notifications sends it from `argocd-notifications-secret` in the `argocd` namespace. Both must hold the same value.

```bash
kubectl create secret generic cost-intel-webhook -n argocd-cost-intel \
  --from-literal=token="$TOKEN"

kubectl create secret generic cost-intel-postgres -n argocd-cost-intel \
  --from-literal=username=cost_user \
  --from-literal=password="$(openssl rand -hex 24)"
```

The notifications side depends on how you install Argo CD, so it's in the steps for each setup below.

> **Warning:** Don't commit these values to Git in plain text. If your Secrets live in Git, create them with the tool you already use, such as Sealed Secrets or External Secrets, using the same names and keys.

## Step 2: Add the Cost Intelligence Application

Add this Application to the Git repository Argo CD watches, next to your other Applications. `features.autoPatchArgoCd: false` turns the hook off, so the chart changes nothing outside its own namespace.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: argocd-cost-intelligence
  namespace: argocd
  finalizers:
  - resources-finalizer.argocd.argoproj.io   # deleting the Application deletes what it installed
spec:
  project: default
  source:
    repoURL: ghcr.io/octopusdeploy/charts   # no oci:// prefix
    chart: argocd-cost-intelligence
    targetRevision: <chart-version>
    helm:
      valuesObject:
        namespace:
          create: false
        features:
          autoPatchArgoCd: false
        webhook:
          existingSecret: cost-intel-webhook
        postgres:
          auth:
            existingSecret: cost-intel-postgres
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd-cost-intel
  syncPolicy:
    automated: {}
```

Set `openCost.baseURL` if OpenCost isn't at `http://opencost.opencost:9003`, and `argoCd.namespace` if Argo CD isn't in `argocd`. The chart's NetworkPolicy uses that namespace to let `argocd-server` and the notifications controller in.

The NetworkPolicy lets Prometheus reach the metrics port only from the namespaces in `metrics.allowedFrom`, by default `monitoring`. If your Prometheus runs in another namespace, set `metrics.allowedFrom` to it, or its scrapes time out and every Cost Intelligence target shows as down. Setting it replaces `monitoring`; see [Prometheus metrics](metrics.md#security). Other settings are in [Configuration](configuration.md).

The chart creates a ClusterRole, so the project must allow cluster-scoped resources. The `default` project does.

If you apply the Application with `kubectl`, Kubernetes may warn that its finalizer should be "a domain-qualified finalizer name". You can ignore it: the Application is created and the finalizer still works.

Who sees the cluster-wide Namespaces, Idle spend and Chargeback pages is decided by this Application: anyone your Argo CD RBAC lets `get` it. See [Choose your settings](installation.md#choose-your-settings) to pick another Application or turn those pages off.

Check that it runs before you change Argo CD:

```bash
kubectl get pods -n argocd-cost-intel
```

You should see the backend, the exporter, the UI and `postgres-0`, all `Running`.

## Step 3: Change Argo CD

Pick the section that matches how you install Argo CD:

- [Argo CD's Helm chart](#argo-cds-helm-chart), the `argo-cd` chart from argo-helm
- [Plain manifests or Kustomize](#plain-manifests-or-kustomize), Argo CD's `install.yaml` with your own patches

Both were checked against Argo CD v3.5.5 and version 10.10.5 of the `argo-cd` Helm chart. The Helm chart section was also checked against Argo CD v3.6.0-rc4, using version 10.11.1 of the chart with `global.image.tag` set to `v3.6.0-rc4`.

### Argo CD's Helm chart

Add these values to the values file of your Argo CD release. Each block below is one part of the table at the top. If your values already set one of these keys, merge the two, as explained after the YAML.

```yaml
configs:
  cm:
    extension.config: |
      extensions:
      - name: cost-intel
        backend:
          services:
          - url: http://argocd-cost-intelligence.argocd-cost-intel.svc.cluster.local:8080

  params:
    server.enable.proxy.extension: true

  rbac:
    policy.cost-intel.csv: |
      p, role:admin, extensions, invoke, cost-intel, allow

server:
  initContainers:
  - name: cost-intel-extension
    image: busybox:1.37.0
    command:
    - sh
    - -c
    - >-
      for i in 1 2 3 4 5; do
      wget -q -T 10 -O /ext/extension-cost-intel.js
      http://argocd-cost-intelligence-ui.argocd-cost-intel.svc.cluster.local:3000/index.js
      && echo OK && exit 0; sleep 10; done;
      echo "Warning: extension download failed"; exit 0
    securityContext:
      runAsUser: 1000
      runAsNonRoot: true
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop: [ALL]
    volumeMounts:
    - name: cost-intel-ext-js
      mountPath: /ext
  volumes:
  - name: cost-intel-ext-js
    emptyDir: {}
  volumeMounts:
  - name: cost-intel-ext-js
    mountPath: /tmp/extensions

notifications:
  notifiers:
    service.webhook.cost-intel: |
      url: http://argocd-cost-intelligence.argocd-cost-intel.svc.cluster.local:8080/api/cost-analysis/on-sync
      headers:
      - name: Content-Type
        value: application/json
      - name: Authorization
        value: Bearer $cost-intel-webhook-token

  triggers:
    trigger.cost-intel-on-sync-succeeded: |
      - when: app.status.operationState.phase in ["Succeeded"] and app.status.operationState.finishedAt != ""
        oncePer: app.status.operationState.finishedAt
        send: [cost-intel-sync-succeeded]

  templates:
    template.cost-intel-sync-succeeded: |
      webhook:
        cost-intel:
          method: POST
          headers:
          - name: Content-Type
            value: application/json
          body: |
            {
              "applicationName": "{{.app.metadata.name}}",
              "applicationNamespace": "{{.app.metadata.namespace}}",
              "namespace": "{{.app.spec.destination.namespace}}",
              "destinationServer": "{{with .app.spec.destination.server}}{{.}}{{end}}",
              "destinationName": "{{with .app.spec.destination.name}}{{.}}{{end}}",
              "resources": [{{range $i, $r := .app.status.resources}}{{if $i}}, {{end}}{"kind": "{{$r.kind}}", "name": "{{$r.name}}", "namespace": "{{with index $r "namespace"}}{{.}}{{end}}"}{{end}}],
              "syncTimestamp": "{{.app.status.operationState.finishedAt}}",
              "syncResult": "Succeeded"
            }

  subscriptions:
    - recipients:
      - cost-intel
      triggers:
      - cost-intel-on-sync-succeeded
```

A few things to check in your existing values:

- `configs.cm.extension.config` holds every proxy extension. If you already have one, add the `cost-intel` entry to its `extensions:` list instead of replacing the key.
- `notifications.subscriptions` is one list for all of Argo CD Notifications. Add the entry above to the list you have.
- Give other roles access with more lines in `policy.cost-intel.csv`, for example `p, role:cost-viewer, extensions, invoke, cost-intel, allow`. They also need `get` on an Application to see its tab, which they usually have already.
- The template above uses `{{ }}` on purpose. Argo CD Notifications fills them in when it sends the webhook, and the Argo CD chart passes these values through unchanged. If your own tooling runs the values file through a template engine first, escape them for that tool.
- `server.extensions` in the same chart runs the [Argo CD extension installer](#use-the-argo-cd-extension-installer-instead) instead of the init container above. Use one or the other.

The token goes into `argocd-notifications-secret`. The chart creates that Secret and fills it from `notifications.secret.items`. If you install the Argo CD chart with Helm, you can put the token there in a values file you don't commit:

```yaml
notifications:
  secret:
    items:
      cost-intel-webhook-token: <the token from step 1>
```

If Argo CD renders its own chart from Git, every values file is in Git, so set `notifications.secret.create: false` and create the Secret with your secret tool instead.

You don't need to add the notifications ClusterRole. The chart already gives the notifications controller access to Applications in every namespace, unless you set `createClusterRoles: false`; then add Argo CD's, as for plain manifests below. It only watches the namespaces in `configs.params."application.namespaces"`, though, as the [apps in any namespace](https://argo-cd.readthedocs.io/en/stable/operator-manual/app-any-namespace/) guide describes.

Sync your Argo CD release. Changing the init container restarts `argocd-server`. If it restarts before Cost Intelligence's UI is running, it starts without the plugin; see [If the tab doesn't appear](#if-the-tab-doesnt-appear).

If the hook has run on this Argo CD before and you upgrade the release yourself with Helm 4, after installing it with Helm 4, the upgrade can fail with `conflict with "kubectl-patch"` on `extension.config`, `subscriptions` or another key from [What you'll add](#what-youll-add). Helm 4 upgrades a release it installed with server-side apply, which won't overwrite a value another tool set, and the hook set these with `kubectl patch`. Run the upgrade again with `--force-conflicts`, so Helm takes those values over. Argo CD syncing its own release doesn't stop on this: its server-side apply always forces conflicts, and client-side apply doesn't check them.

### Plain manifests or Kustomize

These files go in the Kustomize overlay you use to install Argo CD. The ConfigMap patches add keys and leave the ones you have alone.

`kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: argocd
resources:
- https://raw.githubusercontent.com/argoproj/argo-cd/v3.5.5/manifests/install.yaml   # the install you already use
# Only if Applications live outside the argocd namespace, Argo CD's notifications RBAC for them:
# - https://raw.githubusercontent.com/argoproj/argo-cd/v3.5.5/examples/k8s-rbac/argocd-server-applications/argocd-notifications-controller-rbac-clusterrole.yaml
# - https://raw.githubusercontent.com/argoproj/argo-cd/v3.5.5/examples/k8s-rbac/argocd-server-applications/argocd-notifications-controller-rbac-clusterrolebinding.yaml
patches:
- path: cost-intel-argocd-cm.yaml
- path: cost-intel-argocd-cmd-params-cm.yaml
- path: cost-intel-argocd-rbac-cm.yaml
- path: cost-intel-argocd-notifications-cm.yaml
- path: cost-intel-argocd-server.yaml
```

`cost-intel-argocd-cm.yaml`. If `extension.config` is already set in your overlay, add the `cost-intel` entry to its list instead:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cm
data:
  extension.config: |
    extensions:
    - name: cost-intel
      backend:
        services:
        - url: http://argocd-cost-intelligence.argocd-cost-intel.svc.cluster.local:8080
```

`cost-intel-argocd-cmd-params-cm.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cmd-params-cm
data:
  server.enable.proxy.extension: "true"
```

`cost-intel-argocd-rbac-cm.yaml`. Add a line for each other role that should see the plugin:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
data:
  policy.cost-intel.csv: |
    p, role:admin, extensions, invoke, cost-intel, allow
```

`cost-intel-argocd-notifications-cm.yaml`. `subscriptions` is one list for all of Argo CD Notifications, so if your overlay already sets it, add the entry there instead:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-notifications-cm
data:
  service.webhook.cost-intel: |
    url: http://argocd-cost-intelligence.argocd-cost-intel.svc.cluster.local:8080/api/cost-analysis/on-sync
    headers:
    - name: Content-Type
      value: application/json
    - name: Authorization
      value: Bearer $cost-intel-webhook-token
  trigger.cost-intel-on-sync-succeeded: |
    - when: app.status.operationState.phase in ["Succeeded"] and app.status.operationState.finishedAt != ""
      oncePer: app.status.operationState.finishedAt
      send: [cost-intel-sync-succeeded]
  template.cost-intel-sync-succeeded: |
    webhook:
      cost-intel:
        method: POST
        headers:
        - name: Content-Type
          value: application/json
        body: |
          {
            "applicationName": "{{.app.metadata.name}}",
            "applicationNamespace": "{{.app.metadata.namespace}}",
            "namespace": "{{.app.spec.destination.namespace}}",
            "destinationServer": "{{with .app.spec.destination.server}}{{.}}{{end}}",
            "destinationName": "{{with .app.spec.destination.name}}{{.}}{{end}}",
            "resources": [{{range $i, $r := .app.status.resources}}{{if $i}}, {{end}}{"kind": "{{$r.kind}}", "name": "{{$r.name}}", "namespace": "{{with index $r "namespace"}}{{.}}{{end}}"}{{end}}],
            "syncTimestamp": "{{.app.status.operationState.finishedAt}}",
            "syncResult": "Succeeded"
          }
  subscriptions: |
    - recipients:
      - cost-intel
      triggers:
      - cost-intel-on-sync-succeeded
```

`cost-intel-argocd-server.yaml`. If the download fails, the init container still exits cleanly, so `argocd-server` starts without the plugin:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argocd-server
spec:
  template:
    spec:
      initContainers:
      - name: cost-intel-extension
        image: busybox:1.37.0
        command:
        - sh
        - -c
        - >-
          for i in 1 2 3 4 5; do
          wget -q -T 10 -O /ext/extension-cost-intel.js
          http://argocd-cost-intelligence-ui.argocd-cost-intel.svc.cluster.local:3000/index.js
          && echo OK && exit 0; sleep 10; done;
          echo "Warning: extension download failed"; exit 0
        securityContext:
          runAsUser: 1000
          runAsNonRoot: true
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop: [ALL]
        volumeMounts:
        - name: cost-intel-ext-js
          mountPath: /ext
      containers:
      - name: argocd-server
        volumeMounts:
        - name: cost-intel-ext-js
          mountPath: /tmp/extensions
      volumes:
      - name: cost-intel-ext-js
        emptyDir: {}
```

The token goes into `argocd-notifications-secret`, which Argo CD's `install.yaml` creates empty. Add the key with your secret tool, or with `kubectl` if the Secret isn't in Git, in the shell where you set `TOKEN` in step 1:

```bash
kubectl patch secret argocd-notifications-secret -n argocd --type merge \
  -p "{\"stringData\":{\"cost-intel-webhook-token\":\"$TOKEN\"}}"
```

If your Applications live in other namespaces as well as `argocd` ([apps in any namespace](https://argo-cd.readthedocs.io/en/stable/operator-manual/app-any-namespace/)), uncomment the two lines in `resources` above and use the same Argo CD version as your install. `install.yaml` only lets the notifications controller read Applications in `argocd`. Those two lines add Argo CD's `argocd-notifications-controller-cluster-apps` ClusterRole, which lets it read Applications, Secrets and ConfigMaps in every namespace. The controller also has to watch those namespaces, which `application.namespaces` in `argocd-cmd-params-cm` sets.

Sync or apply your overlay. The `argocd-server` patch restarts it. If it restarts before Cost Intelligence's UI is running, it starts without the plugin; see [If the tab doesn't appear](#if-the-tab-doesnt-appear).

### Use the Argo CD extension installer instead

The [Argo CD extension installer](https://github.com/argoproj-labs/argocd-extension-installer) can download the plugin in place of the init container above, and checks its checksum. It needs access to GitHub, or to wherever you host the files. With the Argo CD chart it runs from `server.extensions`; with plain manifests it replaces the init container in `cost-intel-argocd-server.yaml`, with `EXTENSIONS_DIR` set to `/tmp/extensions` (see its README) and Argo CD's `tmp` volume mounted at `/tmp`, where it downloads. Set `EXTENSION_URL` and `EXTENSION_CHECKSUM_URL` to the archive and the checksum file each release attaches:

- `https://github.com/OctopusDeploy/argocd-cost-intelligence/releases/download/v<chart-version>/extension.tar.gz`
- `https://github.com/OctopusDeploy/argocd-cost-intelligence/releases/download/v<chart-version>/extension_checksums.txt`

Set `IGNORE_FAILURE` to `"true"`. Without it, a failed download keeps `argocd-server`, and so the whole Argo CD UI, from starting. If the hook has run on this Argo CD before, its init container has the same name as the installer's, so remove what it added to `argocd-server` first. Run only step 5 of [Uninstall](installation.md#uninstall), and only before you sync the installer in: afterwards it would also remove the `/tmp/extensions` mount the installer needs.

## Check that it works

Run these after both your Argo CD configuration and Cost Intelligence have synced.

Argo CD has the settings:

```bash
kubectl get cm argocd-cm -n argocd -o jsonpath='{.data.extension\.config}'
kubectl get cm argocd-cmd-params-cm -n argocd -o jsonpath='{.data.server\.enable\.proxy\.extension}'
kubectl get cm argocd-rbac-cm -n argocd -o jsonpath='{.data.policy\.cost-intel\.csv}'
kubectl get cm argocd-notifications-cm -n argocd -o jsonpath='{.data.subscriptions}'
```

The first prints the `cost-intel` entry, the second `true`, the third the admin line, and the last includes `cost-intel` and `cost-intel-on-sync-succeeded`.

The token is the same in both Secrets. These two commands print the same value:

```bash
kubectl get secret cost-intel-webhook -n argocd-cost-intel -o jsonpath='{.data.token}' | base64 -d; echo
kubectl get secret argocd-notifications-secret -n argocd -o jsonpath='{.data.cost-intel-webhook-token}' | base64 -d; echo
```

The plugin file is in the `argocd-server` pod. Its Deployment and container names depend on how you installed Argo CD (the Argo CD chart names the Deployment after your release and the container `server`), so the first two lines look them up. Run them once the `argocd-server` rollout has finished, as a pod from before the sync has no plugin:

```bash
POD=$(kubectl get pods -n argocd -l app.kubernetes.io/name=argocd-server -o name | head -1)
CONTAINER=$(kubectl get -n argocd "$POD" -o jsonpath='{.spec.containers[0].name}')
kubectl logs -n argocd "$POD" -c cost-intel-extension
kubectl exec -n argocd "$POD" -c "$CONTAINER" -- ls /tmp/extensions
```

The log says `OK`, or `UI extension installed successfully` with the installer, and the listing shows `extension-cost-intel.js`.

Then open any Application in the Argo CD UI. It has a **Cost Intelligence** tab. Cost Intelligence measures each application when it starts, so most tabs show a cost within a few minutes, marked "Measured at install". Sync an application and its tab shows the new cost within about half a minute. A new application takes a few minutes longer, since OpenCost has no data yet for the namespace its first sync creates; Cost Intelligence measures it again until it does. If an application that already ran doesn't show its cost, see [Costs show $0 or "no data"](operations.md#costs-show-0-or-no-data).

## If the tab doesn't appear

[The Cost Intelligence tab doesn't appear](operations.md#the-cost-intelligence-tab-doesnt-appear) in the operations guide lists the usual causes; skip the ones about the hook. Also check what you added in step 3:

- `argocd-server` restarted before Cost Intelligence's UI was running, or hasn't restarted since you added the init container or changed `server.enable.proxy.extension`. The hook would restart it; here you do: `kubectl rollout restart deployment -n argocd -l app.kubernetes.io/name=argocd-server`. The Argo CD chart names the Deployment after your release, so this finds it by its label.
- The init container's URL has the wrong Service name, namespace or port. Check them with `kubectl get svc -n argocd-cost-intel`, then restart `argocd-server`.
- The file name doesn't start with `extension` and end in `.js`. Argo CD ignores other files in `/tmp/extensions`.

## Upgrade and uninstall

To upgrade, change `targetRevision` in the Cost Intelligence Application. With the installer, change the version in its two URLs as well. Your Argo CD configuration doesn't change otherwise.

To uninstall, remove every key and block you added in step 3, then delete the Cost Intelligence Application. Its finalizer makes Argo CD delete everything the chart installed. Then delete the `argocd-cost-intel` namespace, which also removes the Secrets from step 1 and the database, so back the database up first if you need its history.

Argo CD doesn't delete the webhook token from `argocd-notifications-secret` for you. The Argo CD chart writes it as `stringData`, which only adds and updates keys, so removing it from your values leaves it in the Secret. With plain manifests you added it with `kubectl`, so nothing in Git owns it. Once the notifications entries are gone from your Argo CD configuration and synced, and the token is out of your secret tool if you use one, delete the key. The command fails if the key is already gone, so skip it then:

```bash
kubectl patch secret argocd-notifications-secret -n argocd \
  --type json -p '[{"op":"remove","path":"/data/cost-intel-webhook-token"}]'
```
