# Install when Git manages Argo CD

This guide is for you if Argo CD itself is installed and configured from Git, for example with Argo CD managing its own Helm release or a Kustomize overlay. In that case the chart's install hook is the wrong tool. It changes Argo CD's ConfigMaps and the `argocd-server` Deployment with `kubectl patch`, and those changes live only in the cluster. They are lost when your Git sets the same key, and they don't come back when you rebuild Argo CD from Git. So you turn the hook off and put the same settings in your own Argo CD configuration.

You need everything in [installation.md](installation.md#prerequisites) first: Argo CD 3.4 or 3.5 with Argo CD Notifications, and OpenCost.

## What you'll add

These are all the changes the hook makes to Argo CD. You'll add each of them to your Argo CD configuration.

| Object in the `argocd` namespace | Key or field | Why it's needed |
|---|---|---|
| `argocd-server` Deployment | an init container and a volume at `/tmp/extensions` | Puts the plugin's JavaScript where `argocd-server` loads UI extensions from |
| `argocd-cm` ConfigMap | `extension.config` | Tells Argo CD where to send the plugin's API calls |
| `argocd-cmd-params-cm` ConfigMap | `server.enable.proxy.extension` | Turns on proxy extensions, which are off by default |
| `argocd-rbac-cm` ConfigMap | `policy.cost-intel.csv` | Lets users open the plugin. On Argo CD 3.5 it is denied to everyone until a policy allows it |
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
        # openCost:
        #   baseURL: http://opencost.opencost.svc.cluster.local:9003
        # argoCd:
        #   namespace: argocd
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd-cost-intel
  syncPolicy:
    automated: {}
```

Set `openCost.baseURL` if OpenCost isn't at `http://opencost.opencost:9003`, and `argoCd.namespace` if Argo CD isn't in `argocd`. The chart's NetworkPolicy uses that namespace to let `argocd-server` and the notifications controller in. Other settings are in [configuration.md](configuration.md).

The chart creates a ClusterRole, so the project must allow cluster-scoped resources. The `default` project does.

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

Both were checked against Argo CD v3.5.5 and version 10.10.5 of the `argo-cd` Helm chart.

### Argo CD's Helm chart

Add these values to the values file of your Argo CD release. Each block below is one part of the table at the top. If your values already set one of these keys, merge the two, as explained after the YAML.

```yaml
configs:
  cm:
    # Where Argo CD sends the plugin's API calls
    extension.config: |
      extensions:
      - name: cost-intel
        backend:
          services:
          - url: http://argocd-cost-intelligence.argocd-cost-intel.svc.cluster.local:8080

  params:
    # Proxy extensions are off unless this is set
    server.enable.proxy.extension: true

  rbac:
    # Lets the built-in admin role open the plugin
    policy.cost-intel.csv: |
      p, role:admin, extensions, invoke, cost-intel, allow

server:
  # Downloads the plugin from the Cost Intelligence UI Service when argocd-server starts
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

  # Sends the trigger above to the webhook above for every Application
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

Sync your Argo CD release. Changing the init container restarts `argocd-server`. If the restart happens before Cost Intelligence's UI is running, the download fails, `argocd-server` starts without the plugin, and you restart it later with `kubectl rollout restart deployment -n argocd -l app.kubernetes.io/name=argocd-server`. The chart names the Deployment after your release, so this finds it by its label.

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

`cost-intel-argocd-cm.yaml` tells Argo CD where to send the plugin's API calls. If `extension.config` is already set in your overlay, add the `cost-intel` entry to its list instead.

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

`cost-intel-argocd-cmd-params-cm.yaml` turns on proxy extensions:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cmd-params-cm
data:
  server.enable.proxy.extension: "true"
```

`cost-intel-argocd-rbac-cm.yaml` lets the built-in admin role open the plugin. Add a line for each other role that should see it.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
data:
  policy.cost-intel.csv: |
    p, role:admin, extensions, invoke, cost-intel, allow
```

`cost-intel-argocd-notifications-cm.yaml` calls Cost Intelligence after each successful sync. `subscriptions` is one list for all of Argo CD Notifications, so if your overlay already sets it, add the entry there instead.

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

`cost-intel-argocd-server.yaml` downloads the plugin from the Cost Intelligence UI Service when `argocd-server` starts, into a volume mounted where `argocd-server` loads extensions from. If the download fails, the init container still exits cleanly so `argocd-server` starts without the plugin.

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

If your Applications live in other namespaces as well as `argocd` ([apps in any namespace](https://argo-cd.readthedocs.io/en/stable/operator-manual/app-any-namespace/)), the notifications controller needs to read them there. Argo CD's `install.yaml` only gives it a Role in `argocd`. Argo CD ships a ClusterRole and ClusterRoleBinding for this, `argocd-notifications-controller-cluster-apps`, in `examples/k8s-rbac/argocd-server-applications`. Besides Applications, it lets the controller read Secrets and ConfigMaps in every namespace. Uncomment the two lines in `resources` above and use the same Argo CD version as your install. The controller also has to watch those namespaces, which `application.namespaces` in `argocd-cmd-params-cm` sets, as that guide describes.

Sync or apply your overlay. The `argocd-server` patch restarts it. If the restart happens before Cost Intelligence's UI is running, `argocd-server` starts without the plugin, and you restart it later with `kubectl rollout restart deployment/argocd-server -n argocd`.

### Use the Argo CD extension installer instead

The [Argo CD extension installer](https://github.com/argoproj-labs/argocd-extension-installer) is an init container that downloads an extension archive and checks its checksum. The Argo CD chart runs it from `server.extensions`.

Each Cost Intelligence release from 0.1.0 on attaches the archive, `extension.tar.gz`, and its checksum file, `extension_checksums.txt`. 0.1.0-rc.1 doesn't have them, so use the init container above with it. The URLs below need internet access to GitHub. In an air-gapped cluster, host the two files yourself and point the URLs at them.

If the hook has run on this Argo CD before, remove what it added to `argocd-server` before you sync the installer into it. Its init container has the same name as the installer's, so applying the installer on top of it would keep parts of the old one. The hook can only change `argocd-server` when its Deployment and container both have that name, as in Argo CD's `install.yaml`. The Argo CD chart calls the container `server`, so there the hook added nothing to `argocd-server` and you skip this. Don't run this after that sync: it removes the `/tmp/extensions` mount, which by then is part of your Argo CD configuration:

```bash
kubectl patch deployment argocd-server -n argocd --type strategic -p '{"spec":{"template":{"spec":{"initContainers":[{"name":"cost-intel-extension","$patch":"delete"}],"containers":[{"name":"argocd-server","volumeMounts":[{"mountPath":"/tmp/extensions","$patch":"delete"}]}],"volumes":[{"name":"cost-intel-ext-js","$patch":"delete"}]}}}}'
```

With the Argo CD chart, use this instead of `server.initContainers`, `server.volumes` and `server.volumeMounts`:

```yaml
server:
  extensions:
    enabled: true
    extensionList:
    - name: cost-intel-extension
      env:
      - name: EXTENSION_URL
        value: https://github.com/OctopusDeploy/argocd-cost-intelligence/releases/download/v<chart-version>/extension.tar.gz
      - name: EXTENSION_CHECKSUM_URL
        value: https://github.com/OctopusDeploy/argocd-cost-intelligence/releases/download/v<chart-version>/extension_checksums.txt
      - name: IGNORE_FAILURE
        value: "true"
```

With plain manifests, replace the init container in `cost-intel-argocd-server.yaml` with this one. It also mounts Argo CD's own `tmp` volume, because the installer downloads there and its root filesystem is read-only.

```yaml
      initContainers:
      - name: cost-intel-extension
        image: quay.io/argoprojlabs/argocd-extension-installer:v1.1.0
        env:
        - name: EXTENSION_URL
          value: https://github.com/OctopusDeploy/argocd-cost-intelligence/releases/download/v<chart-version>/extension.tar.gz
        - name: EXTENSION_CHECKSUM_URL
          value: https://github.com/OctopusDeploy/argocd-cost-intelligence/releases/download/v<chart-version>/extension_checksums.txt
        - name: EXTENSIONS_DIR
          value: /tmp/extensions
        - name: IGNORE_FAILURE
          value: "true"
        securityContext:
          runAsUser: 1000
          runAsNonRoot: true
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop: [ALL]
        volumeMounts:
        - name: cost-intel-ext-js
          mountPath: /tmp/extensions
        - name: tmp
          mountPath: /tmp
```

`IGNORE_FAILURE` lets `argocd-server` start without the plugin if the download fails. Without it, a failed download keeps `argocd-server`, and so the whole Argo CD UI, from starting.

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

Then open any Application in the Argo CD UI. It has a **Cost Intelligence** tab. Cost Intelligence measures each application when it starts, so most tabs show a cost within a few minutes, marked "Measured at install". Sync an application and its tab shows the new cost within about half a minute. If it doesn't, check that the webhook was sent:

```bash
kubectl logs -n argocd -l app.kubernetes.io/name=argocd-notifications-controller --tail=-1 | grep cost-intel
```

With `-l`, `kubectl logs` shows only the last 10 lines of each pod unless you add `--tail=-1`.

## If the tab doesn't appear

Go through these in order. Each one is a common reason.

- `argocd-server` hasn't restarted since you added the init container. Restart it with `kubectl rollout restart deployment -n argocd -l app.kubernetes.io/name=argocd-server`.
- The download failed, so `/tmp/extensions` is empty. The `cost-intel-extension` log says why. Usually the UI wasn't running yet, or the URL has the wrong Service name, namespace or port. Check them with `kubectl get svc -n argocd-cost-intel`, then restart `argocd-server`.
- The chart's NetworkPolicy blocks the download because `argoCd.namespace` in the Cost Intelligence values doesn't match the namespace Argo CD runs in.
- The file name doesn't start with `extension` and end in `.js`. Argo CD ignores other files in `/tmp/extensions`.
- The tab is there but shows "401: Unauthorized extension request". The RBAC rule is missing or doesn't name your role. Check `policy.cost-intel.csv`, and that the extension name in it is `cost-intel`.
- The tab is there but shows an error instead of costs. Check that `server.enable.proxy.extension` is `"true"` and that `argocd-server` restarted after it changed, because it reads that setting only when it starts. Then check that the URL in `extension.config` points at the backend Service on port 8080, not the UI Service.
- The tab never fills in after a sync. The webhook isn't reaching Cost Intelligence. Check the subscription and the token from above. If `kubectl logs -n argocd-cost-intel deploy/argocd-cost-intelligence | grep 'Refused POST /on-sync'` prints anything, the two tokens differ. If both look right, restart the notifications controller with `kubectl rollout restart deployment -n argocd -l app.kubernetes.io/name=argocd-notifications-controller`, as the hook does after it changes them.

[operations.md](operations.md) covers more problems once the tab is there.

## Upgrade and uninstall

To upgrade, change `targetRevision` in the Cost Intelligence Application. With the installer, change the version in its two URLs as well. Your Argo CD configuration doesn't change otherwise.

To uninstall, remove every key and block you added in step 3, then delete the Cost Intelligence Application. Its finalizer makes Argo CD delete everything the chart installed. Then delete the `argocd-cost-intel` namespace, which also removes the Secrets from step 1 and the database, so back the database up first if you need its history.
