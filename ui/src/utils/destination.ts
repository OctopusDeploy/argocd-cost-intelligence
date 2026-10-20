// An Application's destination, as Argo CD has it in spec.destination.
export interface Destination {
  server?: string;
  name?: string;
  namespace?: string;
}

// The cluster the service runs in, as Argo CD names it.
export const LOCAL_SERVER = 'https://kubernetes.default.svc';
export const LOCAL_NAME = 'in-cluster';

// otherCluster returns the cluster an application deploys to when it is not the one Cost Intelligence
// runs in, by its name or else its server, or null when it deploys here. The rule is the service's:
// local means the server is empty or LOCAL_SERVER and the name is empty or LOCAL_NAME.
export function otherCluster(destination?: Destination): string | null {
  const server = destination?.server ?? '';
  const name = destination?.name ?? '';
  if ((server === '' || server === LOCAL_SERVER) && (name === '' || name === LOCAL_NAME)) {
    return null;
  }
  return name || server;
}
