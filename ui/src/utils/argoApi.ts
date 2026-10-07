import { ArgoApplication } from './namespaceApps';

interface ApplicationList {
  items?: Array<{
    metadata?: { name?: string; namespace?: string };
    status?: { resources?: Array<{ kind?: string; name?: string; namespace?: string }> };
  }> | null;
}

// fetchArgoApplications lists the applications the signed-in user can see, with the resources each
// one manages. It calls Argo CD's own API with the session cookie, so Argo CD's RBAC applies.
export async function fetchArgoApplications(): Promise<ArgoApplication[]> {
  const response = await fetch(
    '/api/v1/applications?fields=items.metadata.name,items.metadata.namespace,items.status.resources'
  );
  if (!response.ok) throw new Error(`${response.status}: ${response.statusText}`);

  const body: ApplicationList = await response.json();
  return (body.items ?? []).flatMap(item =>
    item.metadata?.name
      ? [
          {
            name: item.metadata.name,
            namespace: item.metadata.namespace ?? '',
            resources: (item.status?.resources ?? []).flatMap(resource =>
              resource.kind && resource.name
                ? [{ kind: resource.kind, name: resource.name, namespace: resource.namespace }]
                : []
            ),
          },
        ]
      : []
  );
}
