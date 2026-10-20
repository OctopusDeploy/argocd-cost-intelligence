#!/usr/bin/env node

/**
 * Mock OpenCost Server for Local Testing
 *
 * Simulates the OpenCost API endpoints for local development and testing
 * without needing a real Kubernetes cluster with metrics.
 *
 * Provides fake but realistic cost data for testing cost-intelligence backend. Outside a cluster
 * (docker-compose, CI) it serves fixed pods; in one, the namespace's real pods with made-up costs
 * (see "Pods from the cluster" below).
 */

const fs = require('fs');
const http = require('http');
const https = require('https');
const url = require('url');

// Mock cost data by namespace
const mockCosts = {
  default: {
    window: ['2026-06-13T00:00:00Z', '2026-06-14T00:00:00Z'],
    allocations: {
      'pod:nginx-7d4b8c-xkqp2': {
        cpuCoreHours: 0.25,
        cpuCost: 15.08,
        ramByteHours: 268435456,
        ramCost: 7.53,
        networkCost: 0.60,
        loadBalancerCost: 0.00,
        pvCost: 0.25,
        totalCost: 23.46,
        controllerKind: 'Deployment',
        controller: 'nginx',
      },
      'pod:nginx-7d4b8c-mrt91': {
        cpuCoreHours: 0.25,
        cpuCost: 15.07,
        ramByteHours: 268435456,
        ramCost: 7.52,
        networkCost: 0.60,
        loadBalancerCost: 0.00,
        pvCost: 0.25,
        totalCost: 23.44,
        controllerKind: 'Deployment',
        controller: 'nginx',
      },
      'pod:redis-master-0': {
        cpuCoreHours: 0.7,
        cpuCost: 42.21,
        ramByteHours: 751619276,
        ramCost: 28.18,
        networkCost: 1.47,
        loadBalancerCost: 3.50,
        pvCost: 2.10,
        totalCost: 77.46,
        controllerKind: 'StatefulSet',
        controller: 'redis-master',
      },
      'pod:redis-replica-0': {
        cpuCoreHours: 0.15,
        cpuCost: 9.05,
        ramByteHours: 161061273,
        ramCost: 6.04,
        networkCost: 0.32,
        loadBalancerCost: 0.75,
        pvCost: 0.45,
        totalCost: 16.61,
        controllerKind: 'StatefulSet',
        controller: 'redis-replica',
      },
      'pod:redis-replica-1': {
        cpuCoreHours: 0.15,
        cpuCost: 9.04,
        ramByteHours: 161061273,
        ramCost: 6.03,
        networkCost: 0.31,
        loadBalancerCost: 0.75,
        pvCost: 0.45,
        totalCost: 16.58,
        cpuCoreUsageAverage: 0.09,
        ramByteUsageAverage: 134217728,
        controllerKind: 'StatefulSet',
        controller: 'redis-replica',
      },
      // Orphaned pods: zero CPU/RAM usage but non-zero storage or LB cost
      'pod:evicted-job-worker-xk2p9': {
        cpuCoreHours: 0,
        cpuCost: 0,
        ramByteHours: 0,
        ramCost: 0,
        networkCost: 0,
        loadBalancerCost: 0,
        pvCost: 0.14,
        totalCost: 0.14,
        cpuCoreUsageAverage: 0,
        ramByteUsageAverage: 0,
        controllerKind: 'Job',
        controller: 'data-migration-job',
      },
      'pod:stuck-pending-app-r9k1m': {
        cpuCoreHours: 0,
        cpuCost: 0,
        ramByteHours: 0,
        ramCost: 0,
        networkCost: 0,
        loadBalancerCost: 0.07,
        pvCost: 0,
        totalCost: 0.07,
        cpuCoreUsageAverage: 0,
        ramByteUsageAverage: 0,
        controllerKind: 'Deployment',
        controller: 'frontend',
      },
    },
  },
  production: {
    window: ['2026-06-13T00:00:00Z', '2026-06-14T00:00:00Z'],
    allocations: {
      'pod:app-server': {
        cpuCoreHours: 2.0,
        cpuCost: 120.60,
        ramByteHours: 2147483648,
        ramCost: 80.50,
        networkCost: 5.00,
        loadBalancerCost: 10.00,
        pvCost: 8.00,
        totalCost: 224.10,
      },
    },
  },
};

// Helper to calculate monthly cost from daily cost
function annualizeCost(dailyCost) {
  return Math.round(dailyCost * 30 * 100) / 100;
}

// Helper: format a Date as RFC3339 without milliseconds
function toRFC3339(date) {
  return date.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

const DAY_MS = 24 * 60 * 60 * 1000;
// Like OpenCost on a default Prometheus, the mock has data only for the last RETENTION_DAYS days.
const RETENTION_DAYS = 15;

// dailySteps lists the UTC days a step=1d query covers that the mock has data for: the last N days
// for "Nd", or each day from the start of an "<start>,<end>" RFC3339 window up to its end.
function dailySteps(windowStr) {
  const today = new Date();
  today.setUTCHours(0, 0, 0, 0);
  let first;
  let last = today;
  if (windowStr && windowStr.includes(',')) {
    const [start, end] = windowStr.split(',').map((value) => new Date(value));
    first = new Date(start);
    first.setUTCHours(0, 0, 0, 0);
    last = new Date(end.getTime() - 1);
    last.setUTCHours(0, 0, 0, 0);
  } else {
    first = new Date(today.getTime() - ((parseInt(windowStr || '30d') || 30) - 1) * DAY_MS);
  }
  const oldest = today.getTime() - (RETENTION_DAYS - 1) * DAY_MS;
  const steps = [];
  for (let day = first.getTime(); day <= last.getTime(); day += DAY_MS) {
    if (day >= oldest && day <= today.getTime()) steps.push(new Date(day));
  }
  return steps;
}

// windowRange reads an OpenCost window as epoch milliseconds: "<start>,<end>" in RFC3339, or a
// duration such as "24h" or "7d" that ends now.
function windowRange(windowStr) {
  const now = Date.now();
  if (windowStr && windowStr.includes(',')) {
    const [start, end] = windowStr.split(',').map((value) => Date.parse(value));
    if (start < end) return { start, end };
  }
  const match = /^(\d+)([hd])$/.exec(windowStr || '');
  const length = match ? parseInt(match[1], 10) * (match[2] === 'h' ? 3600 * 1000 : DAY_MS) : DAY_MS;
  return { start: now - length, end: now };
}

function namespaceOf(queryParams) {
  return queryParams.filter ? queryParams.filter.replace('namespace:"', '').replace('"', '') : 'default';
}

// --- Pods from the cluster ---------------------------------------------------------------------------
//
// In a cluster, with a service account that may list pods and replicasets (deploy/mock-opencost.yaml),
// the mock reports the pods that actually run in the namespace it is asked about, owned by their real
// controllers the way OpenCost names them, so an Argo CD application's own workloads match and its
// figures have values. The costs are made up but stable: each pod's come from its name, its requests
// and how long it ran in the window. Outside a cluster, or for a namespace without pods, the mock
// serves the fixed pods above.

const SA_DIR = '/var/run/secrets/kubernetes.io/serviceaccount';
const IN_CLUSTER = Boolean(process.env.KUBERNETES_SERVICE_HOST) && fs.existsSync(`${SA_DIR}/token`);
const POD_CACHE_MS = 30 * 1000;
const HOUR_MS = 3600 * 1000;
const MiB = 1024 * 1024;
const GiB = 1024 * MiB;
// PRICE_MULTIPLIER scales every price below, because a local cluster's small pods cost cents at
// OpenCost's real prices and the UI is easier to judge with larger figures.
const PRICE_MULTIPLIER = (() => {
  const value = Number(process.env.PRICE_MULTIPLIER || 1);
  if (Number.isFinite(value) && value > 0) return value;
  console.warn(`Ignoring PRICE_MULTIPLIER=${process.env.PRICE_MULTIPLIER}: not a positive number`);
  return 1;
})();
// OpenCost's default on-demand prices.
const CPU_CORE_HOURLY = 0.031611 * PRICE_MULTIPLIER;
const RAM_GIB_HOURLY = 0.004237 * PRICE_MULTIPLIER;
// A claim is priced as 8 GiB at $0.04 per GiB-month.
const PVC_HOURLY = ((8 * 0.04) / 720) * PRICE_MULTIPLIER;

function kubeGet(path) {
  return new Promise((resolve, reject) => {
    const req = https.request({
      host: process.env.KUBERNETES_SERVICE_HOST,
      port: process.env.KUBERNETES_SERVICE_PORT || 443,
      path,
      ca: fs.readFileSync(`${SA_DIR}/ca.crt`),
      // Read on every call: a projected token is rotated.
      headers: { Authorization: `Bearer ${fs.readFileSync(`${SA_DIR}/token`, 'utf8').trim()}` },
      timeout: 5000,
    }, (res) => {
      let body = '';
      res.on('error', reject);
      res.on('aborted', () => reject(new Error(`GET ${path}: response aborted`)));
      res.on('data', (chunk) => { body += chunk; });
      res.on('end', () => {
        if (res.statusCode !== 200) {
          reject(new Error(`GET ${path}: ${res.statusCode} ${body.slice(0, 200)}`));
          return;
        }
        try {
          resolve(JSON.parse(body));
        } catch (err) {
          reject(err);
        }
      });
    });
    req.on('timeout', () => req.destroy(new Error(`GET ${path}: timed out`)));
    req.on('error', reject);
    req.end();
  });
}

// hash maps a string to a stable number in [0, 1).
function hash(text) {
  let h = 2166136261;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return (h >>> 0) / 4294967296;
}

function cores(quantity) {
  if (!quantity) return 0;
  return quantity.endsWith('m') ? parseFloat(quantity) / 1000 : parseFloat(quantity);
}

const BYTE_UNITS = { Ki: 1024, Mi: MiB, Gi: GiB, Ti: 1024 * GiB, k: 1e3, M: 1e6, G: 1e9, T: 1e12 };

function bytes(quantity) {
  const match = /^([0-9.]+(?:e[0-9]+)?)([A-Za-z]*)$/.exec(quantity || '');
  return match ? parseFloat(match[1]) * (BYTE_UNITS[match[2]] || 1) : 0;
}

// ownerOf names a pod's controller as OpenCost does: a ReplicaSet is reported as the Deployment that
// owns it, and as itself otherwise (an Argo Rollout's, for one); a CronJob's pod as its Job.
function ownerOf(pod, replicaSets) {
  const ref = (pod.metadata.ownerReferences || []).find((r) => r.controller);
  if (!ref) return { kind: '', name: '' };
  if (ref.kind === 'ReplicaSet') {
    const rs = replicaSets.get(ref.name);
    const parent = rs && (rs.metadata.ownerReferences || []).find((r) => r.controller);
    if (parent && parent.kind === 'Deployment') return { kind: 'deployment', name: parent.name };
  }
  return { kind: ref.kind.toLowerCase(), name: ref.name };
}

// latest returns the latest of the timestamps, or 0 when none parses.
function latest(timestamps) {
  return Math.max(0, ...timestamps.map((t) => Date.parse(t) || 0));
}

// describePod returns what pricing a pod needs. A finished pod ends when its last container did, or,
// when none reports that (an evicted pod), at its last condition change, and never before it started.
function describePod(pod, replicaSets) {
  const start = Date.parse(pod.status.startTime);
  const done = pod.status.phase === 'Succeeded' || pod.status.phase === 'Failed';
  const finishedAt = done
    ? latest((pod.status.containerStatuses || []).map((s) => s.state?.terminated?.finishedAt))
      || latest((pod.status.conditions || []).map((c) => c.lastTransitionTime))
    : 0;
  return {
    name: pod.metadata.name,
    owner: ownerOf(pod, replicaSets),
    start,
    end: done ? Math.max(start, finishedAt) : Infinity,
    claims: (pod.spec.volumes || []).filter((v) => v.persistentVolumeClaim).length,
    containers: pod.spec.containers.map((c) => ({
      name: c.name,
      cpuRequest: cores(c.resources?.requests?.cpu),
      cpuLimit: cores(c.resources?.limits?.cpu),
      ramRequest: bytes(c.resources?.requests?.memory),
      ramLimit: bytes(c.resources?.limits?.memory),
    })),
  };
}

const podCache = new Map();

const NAMESPACE_NAME = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;

// clusterPods lists the namespace's pods that have started, or returns null outside a cluster or when
// the API can't be read. Listings with pods are cached for POD_CACHE_MS, as the plugin refreshes every
// 30s; an empty one or a failure is not, so a namespace's first pods show up at the next query.
async function clusterPods(namespace) {
  if (!IN_CLUSTER || !NAMESPACE_NAME.test(namespace)) return null;
  const cached = podCache.get(namespace);
  if (cached && Date.now() - cached.at < POD_CACHE_MS) return cached.pods;
  let pods = null;
  try {
    const [podList, rsList] = await Promise.all([
      kubeGet(`/api/v1/namespaces/${namespace}/pods`),
      kubeGet(`/apis/apps/v1/namespaces/${namespace}/replicasets`),
    ]);
    const replicaSets = new Map(rsList.items.map((rs) => [rs.metadata.name, rs]));
    pods = podList.items.filter((pod) => Date.parse(pod.status?.startTime)).map((pod) => describePod(pod, replicaSets));
  } catch (err) {
    console.error(`Listing pods in ${namespace} failed, serving the fixed pods: ${err.message}`);
  }
  if (pods && pods.length > 0) podCache.set(namespace, { at: Date.now(), pods });
  return pods;
}

// podAllocations prices each pod for the part of [start, end) it ran. With wholeWindow, a running pod
// is priced for all of it and a finished one (a Job's) for as long as it ran, so a daily series
// doesn't charge a minute-long Job for a whole day. Each container is charged the larger of its
// request and a made-up use, which runs at 15-85% of the CPU request and 30-90% of the memory one. A
// container without a request is given one, 1.5-4 times a made-up use, so every workload has
// efficiency, idle spend and rightsizing to show.
function podAllocations(pods, start, end, wholeWindow = false) {
  const allocations = {};
  for (const pod of pods) {
    let from = Math.max(start, pod.start);
    let to = Math.min(end, pod.end);
    if (wholeWindow) {
      from = start;
      to = pod.end === Infinity ? end : start + Math.min(end - start, pod.end - pod.start);
    }
    if (to <= from) continue;
    const hours = (to - from) / HOUR_MS;
    const containers = pod.containers.map((c) => {
      const seed = hash(`${pod.name}/${c.name}`);
      const headroom = 1.5 + 2.5 * hash(`${c.name}/${pod.name}`);
      const cpuUsage = c.cpuRequest > 0 ? c.cpuRequest * (0.15 + 0.7 * seed) : 0.01 + 0.09 * seed;
      const ramUsage = c.ramRequest > 0 ? c.ramRequest * (0.3 + 0.6 * seed) : (32 + 160 * seed) * MiB;
      const cpuRequest = c.cpuRequest || cpuUsage * headroom;
      const ramRequest = c.ramRequest || ramUsage * headroom;
      const cpuCoreHours = Math.max(cpuRequest, cpuUsage) * hours;
      const ramByteHours = Math.max(ramRequest, ramUsage) * hours;
      return {
        ...c,
        cpuRequest,
        ramRequest,
        cpuUsage,
        ramUsage,
        cpuCoreHours,
        ramByteHours,
        cpuCost: cpuCoreHours * CPU_CORE_HOURLY,
        ramCost: (ramByteHours / GiB) * RAM_GIB_HOURLY,
      };
    });
    const sum = (field) => containers.reduce((total, c) => total + c[field], 0);
    const networkCost = (0.0002 + 0.002 * hash(pod.name)) * hours * PRICE_MULTIPLIER;
    const pvCost = pod.claims * PVC_HOURLY * hours;
    allocations[`pod:${pod.name}`] = {
      start: toRFC3339(new Date(from)),
      end: toRFC3339(new Date(to)),
      minutes: hours * 60,
      cpuCoreHours: sum('cpuCoreHours'),
      cpuCost: sum('cpuCost'),
      ramByteHours: sum('ramByteHours'),
      ramCost: sum('ramCost'),
      networkCost,
      loadBalancerCost: 0,
      pvCost,
      totalCost: sum('cpuCost') + sum('ramCost') + networkCost + pvCost,
      cpuCoreUsageAverage: sum('cpuUsage'),
      ramByteUsageAverage: sum('ramUsage'),
      cpuCoreRequestAverage: sum('cpuRequest'),
      ramByteRequestAverage: sum('ramRequest'),
      cpuCoreLimitAverage: sum('cpuLimit'),
      ramByteLimitAverage: sum('ramLimit'),
      controllerKind: pod.owner.kind,
      controller: pod.owner.name,
      containers,
    };
  }
  return allocations;
}

// costsFor returns the pods to report for a namespace over a window: the cluster's when it has any,
// the fixed ones otherwise. Like OpenCost, the namespace's own window covers only the time its pods
// ran, which the backend projects to a month from.
async function costsFor(namespace, windowStr) {
  const pods = await clusterPods(namespace);
  if (pods && pods.length > 0) {
    const { start, end } = windowRange(windowStr);
    const ran = pods.filter((pod) => pod.start < end && pod.end > start);
    const from = ran.length > 0 ? Math.max(start, Math.min(...ran.map((pod) => pod.start))) : start;
    const to = ran.length > 0 ? Math.min(end, Math.max(...ran.map((pod) => pod.end))) : end;
    return {
      live: true,
      pods,
      window: [toRFC3339(new Date(from)), toRFC3339(new Date(to))],
      allocations: podAllocations(pods, start, end),
    };
  }
  return mockCosts[namespace] || mockCosts['default'];
}

// containersOf lists a pod's containers. A fixed pod has one, named after its controller, with
// requests above its use.
function containersOf(podName, alloc) {
  if (alloc.containers) return alloc.containers;
  const cpuUsage = alloc.cpuCoreUsageAverage ?? (alloc.cpuCoreHours > 0 ? alloc.cpuCoreHours / 24 : 0);
  const ramUsage = alloc.ramByteUsageAverage ?? (alloc.ramByteHours > 0 ? alloc.ramByteHours / 24 : 0);
  return [{
    name: alloc.controller || podName,
    cpuRequest: cpuUsage * 2.5,
    cpuLimit: cpuUsage * 5,
    ramRequest: ramUsage * 1.4,
    ramLimit: ramUsage * 2,
    cpuUsage,
    ramUsage,
    cpuCoreHours: alloc.cpuCoreHours,
    ramByteHours: alloc.ramByteHours,
    cpuCost: alloc.cpuCost,
    ramCost: alloc.ramCost,
  }];
}

// Handler for /allocation endpoint
function handleAllocation(queryParams, costs) {
  const namespace = namespaceOf(queryParams);
  const aggregate = queryParams.aggregate || 'namespace';
  // Fixed pods hold a month's cost for the daily series; the cluster's are priced per window, so a
  // daily series prices them for a whole day.
  const perDay = costs.live ? 1 : 30;
  const daily = costs.live && queryParams.step === '1d'
    ? podAllocations(costs.pods, 0, DAY_MS, true)
    : costs.allocations;

  // Build response based on aggregation type
  if (aggregate === 'namespace') {
    // Daily time-series mode: step=1d
    if (queryParams.step === '1d') {

      let baseCPU = 0, baseRAM = 0, baseNetwork = 0, baseLB = 0, basePV = 0;
      Object.values(daily).forEach((alloc) => {
        baseCPU += alloc.cpuCost;
        baseRAM += alloc.ramCost;
        baseNetwork += alloc.networkCost || 0;
        baseLB += alloc.loadBalancerCost || 0;
        basePV += alloc.pvCost || 0;
      });
      baseCPU /= perDay; baseRAM /= perDay; baseNetwork /= perDay; baseLB /= perDay; basePV /= perDay;

      const data = [];
      for (const start of dailySteps(queryParams.window)) {
        const end = new Date(start.getTime() + DAY_MS);

        // Gentle weekly pattern: ±15% variance based on day-of-week
        const dow = start.getUTCDay(); // 0=Sun
        const factor = [0.85, 1.00, 1.05, 1.10, 1.08, 1.03, 0.90][dow];

        const cpu = Math.round(baseCPU * factor * 1000) / 1000;
        const ram = Math.round(baseRAM * factor * 1000) / 1000;
        const network = Math.round(baseNetwork * factor * 1000) / 1000;
        const lb = Math.round(baseLB * factor * 1000) / 1000;
        const pv = Math.round(basePV * factor * 1000) / 1000;

        const total = Math.round((cpu + ram + network + lb + pv) * 1000) / 1000;
        data.push({
          [namespace]: {
            start: toRFC3339(start),
            end: toRFC3339(end),
            cpuCost: cpu,
            ramCost: ram,
            networkCost: network,
            loadBalancerCost: lb,
            pvCost: pv,
            totalCost: total,
            carbonCost: Math.round(total * 0.018 * 1000) / 1000,
          },
        });
      }

      return { code: 200, data };
    }

    let totalCPU = 0, totalRAM = 0, cpuCoreHours = 0, ramByteHours = 0, cpuUsage = 0, ramUsage = 0;
    Object.values(costs.allocations).forEach((alloc) => {
      totalCPU += alloc.cpuCost;
      totalRAM += alloc.ramCost;
      cpuCoreHours += alloc.cpuCoreHours || 0;
      ramByteHours += alloc.ramByteHours || 0;
      cpuUsage += alloc.cpuCoreUsageAverage || 0;
      ramUsage += alloc.ramByteUsageAverage || 0;
    });

    let totalNetwork = 0, totalLB = 0, totalPV = 0;
    Object.values(costs.allocations).forEach((alloc) => {
      totalNetwork += alloc.networkCost || 0;
      totalLB += alloc.loadBalancerCost || 0;
      totalPV += alloc.pvCost || 0;
    });

    const totalCost = totalCPU + totalRAM + totalNetwork + totalLB + totalPV;
    const sharedCost = Math.round(totalCost * 0.08 * 1000) / 1000;
    return {
      code: 200,
      data: [
        {
          [namespace]: {
            start: costs.window[0],
            end: costs.window[1],
            cpuCoreHours: costs.live ? cpuCoreHours : 3.5,
            cpuCost: totalCPU,
            ramByteHours: costs.live ? ramByteHours : 3758096384,
            ramCost: totalRAM,
            networkCost: totalNetwork,
            networkCrossZoneCost: Math.round(totalNetwork * 0.40 * 1000) / 1000,
            networkCrossRegionCost: Math.round(totalNetwork * 0.30 * 1000) / 1000,
            networkInternetCost: Math.round(totalNetwork * 0.20 * 1000) / 1000,
            networkNatGatewayEgressCost: Math.round(totalNetwork * 0.07 * 1000) / 1000,
            networkNatGatewayIngressCost: Math.round(totalNetwork * 0.03 * 1000) / 1000,
            loadBalancerCost: totalLB,
            pvCost: totalPV,
            totalCost,
            carbonCost: Math.round(totalCost * 0.018 * 1000) / 1000,
            sharedCost,
            sharedCostBreakdown: {
              'monitoring/prometheus':     { totalCost: Math.round(sharedCost * 0.50 * 1000) / 1000 },
              'kube-system/coredns':       { totalCost: Math.round(sharedCost * 0.31 * 1000) / 1000 },
              'cert-manager/cert-manager': { totalCost: Math.round(sharedCost * 0.19 * 1000) / 1000 },
            },
            rawAllocationOnly: costs.live
              ? { cpuCoreUsageMax: cpuUsage * 1.6, ramByteUsageMax: ramUsage * 1.3 }
              : {
                cpuCoreUsageMax: Math.round(totalCPU / 30 * 2.5 * 1000) / 1000,
                ramByteUsageMax: Math.round(totalRAM / 30 * 2.0 * 1000) / 1000,
              },
          },
        },
      ],
    };
  } else if (aggregate.startsWith('label:')) {
    const now = new Date();
    const start = new Date(now.getTime() - 24 * 60 * 60 * 1000);
    const labelData = {
      'platform': {
        cpuCoreHours: 1.60, cpuCost: 20.67, ramByteHours: 1610612736, ramCost: 16.01,
        pvCost: 2.67, networkCost: 1.33, loadBalancerCost: 0.00, totalCost: 40.68,
        start: toRFC3339(start), end: toRFC3339(now),
      },
      'payments': {
        cpuCoreHours: 1.15, cpuCost: 14.83, ramByteHours: 1073741824, ramCost: 11.01,
        pvCost: 1.50, networkCost: 0.89, loadBalancerCost: 0.25, totalCost: 28.48,
        start: toRFC3339(start), end: toRFC3339(now),
      },
      'data-engineering': {
        cpuCoreHours: 0.72, cpuCost: 9.35, ramByteHours: 751619276, ramCost: 7.34,
        pvCost: 1.20, networkCost: 0.55, loadBalancerCost: 0.00, totalCost: 18.44,
        start: toRFC3339(start), end: toRFC3339(now),
      },
      'infrastructure': {
        cpuCoreHours: 0.40, cpuCost: 5.23, ramByteHours: 429496729, ramCost: 4.01,
        pvCost: 0.80, networkCost: 0.30, loadBalancerCost: 0.10, totalCost: 10.44,
        start: toRFC3339(start), end: toRFC3339(now),
      },
    };
    return { code: 200, data: [labelData] };
  } else if (aggregate === 'controller' && queryParams.step === '1d') {
    // An application's daily series: one entry per controller per day, as real OpenCost reports it.
    const byController = {};
    Object.values(daily).forEach((alloc) => {
      if (!alloc.controller) return;
      const kind = (alloc.controllerKind || 'Deployment').toLowerCase();
      const key = `${kind}:${alloc.controller}`;
      const entry = byController[key] || (byController[key] = { kind, name: alloc.controller, cpu: 0, ram: 0, network: 0, pv: 0 });
      entry.cpu += alloc.cpuCost / perDay;
      entry.ram += alloc.ramCost / perDay;
      entry.network += (alloc.networkCost || 0) / perDay;
      entry.pv += (alloc.pvCost || 0) / perDay;
    });

    const data = [];
    for (const start of dailySteps(queryParams.window)) {
      const end = new Date(start.getTime() + DAY_MS);
      const factor = [0.85, 1.00, 1.05, 1.10, 1.08, 1.03, 0.90][start.getUTCDay()];
      const day = {};
      Object.entries(byController).forEach(([key, c]) => {
        const round = (v) => Math.round(v * factor * 1000) / 1000;
        const cpu = round(c.cpu), ram = round(c.ram), network = round(c.network), pv = round(c.pv);
        day[key] = {
          start: toRFC3339(start),
          end: toRFC3339(end),
          cpuCost: cpu,
          ramCost: ram,
          networkCost: network,
          pvCost: pv,
          totalCost: Math.round((cpu + ram + network + pv) * 1000) / 1000,
          properties: { namespace, controller: c.name, controllerKind: c.kind },
        };
      });
      data.push(day);
    }
    return { code: 200, data };
  } else if (aggregate === 'pod') {
    const pods = {};
    Object.entries(costs.allocations).forEach(([key, { containers, ...alloc }]) => {
      const podName = key.replace('pod:', '');
      const cpuUsage = alloc.cpuCoreUsageAverage ?? (alloc.cpuCoreHours > 0 ? alloc.cpuCoreHours / 24 : 0);
      const ramUsage = alloc.ramByteUsageAverage ?? (alloc.ramByteHours > 0 ? alloc.ramByteHours / 24 : 0);
      pods[podName] = {
        ...alloc,
        minutes: alloc.minutes ?? 24 * 60,
        cpuCoreUsageAverage: cpuUsage,
        ramByteUsageAverage: ramUsage,
        // Requests above use, so an application's efficiency and idle spend have something to show.
        cpuCoreRequestAverage: alloc.cpuCoreRequestAverage ?? cpuUsage * 2.5,
        ramByteRequestAverage: alloc.ramByteRequestAverage ?? ramUsage * 1.4,
        properties: {
          pod: podName,
          namespace,
          controllerKind: alloc.controllerKind ?? 'Deployment',
          controller: alloc.controller ?? podName,
        },
      };
    });

    return {
      code: 200,
      data: [pods],
    };
  } else if (aggregate === 'pod,container') {
    // Each container of each pod, keyed "<pod>/<container>" as OpenCost keys a two-part aggregation.
    const data = {};
    Object.entries(costs.allocations).forEach(([key, alloc]) => {
      const pod = key.replace('pod:', '');
      for (const c of containersOf(pod, alloc)) {
        data[`${pod}/${c.name}`] = {
          name: `${pod}/${c.name}`,
          start: alloc.start || costs.window[0],
          end: alloc.end || costs.window[1],
          minutes: alloc.minutes ?? 24 * 60,
          cpuCoreHours: c.cpuCoreHours,
          cpuCost: c.cpuCost,
          ramByteHours: c.ramByteHours,
          ramCost: c.ramCost,
          totalCost: c.cpuCost + c.ramCost,
          cpuCoreRequestAverage: c.cpuRequest,
          cpuCoreUsageAverage: c.cpuUsage,
          cpuCoreLimitAverage: c.cpuLimit,
          ramByteRequestAverage: c.ramRequest,
          ramByteUsageAverage: c.ramUsage,
          ramByteLimitAverage: c.ramLimit,
          properties: {
            namespace,
            pod,
            container: c.name,
            controllerKind: alloc.controllerKind ?? 'Deployment',
            controller: alloc.controller ?? pod,
          },
        };
      }
    });
    return { code: 200, data: [data] };
  }

  return { code: 400, data: { error: 'Invalid aggregation type' } };
}

// Handler for /allocation/summary endpoint
function handleAllocationSummary(queryParams, costs) {
  const aggregate = queryParams.aggregate || 'namespace';

  if (aggregate.startsWith('label:')) {
    const win = { start: toRFC3339(new Date(Date.now() - 86400000)), end: toRFC3339(new Date()) };
    return {
      code: 200,
      data: {
        step: 86400000000000,
        sets: [{
          window: win,
          // cpuCostIdle and ramCostIdle are 0, as real OpenCost returns them.
          allocations: {
            'platform':         { cpuCoreRequestAverage: 2.0, cpuCoreUsageAverage: 1.24, cpuCoreLimitAverage: 4.0, cpuCost: 20.67, cpuCostIdle: 0, ramByteRequestAverage: 2147483648, ramByteUsageAverage: 1342177280, ramByteLimitAverage: 4294967296, ramCost: 16.01, ramCostIdle: 0, efficiency: 0.62 },
            'payments':         { cpuCoreRequestAverage: 1.5, cpuCoreUsageAverage: 1.07, cpuCoreLimitAverage: 3.0, cpuCost: 14.83, cpuCostIdle: 0,  ramByteRequestAverage: 1073741824, ramByteUsageAverage:  805306368, ramByteLimitAverage: 2147483648, ramCost: 11.01, ramCostIdle: 0, efficiency: 0.71 },
            'data-engineering': { cpuCoreRequestAverage: 1.0, cpuCoreUsageAverage: 0.55, cpuCoreLimitAverage: 2.0, cpuCost: 9.35,  cpuCostIdle: 0,  ramByteRequestAverage:  536870912, ramByteUsageAverage:  268435456, ramByteLimitAverage: 1073741824, ramCost: 7.34,  ramCostIdle: 0, efficiency: 0.55 },
            'infrastructure':   { cpuCoreRequestAverage: 0.8, cpuCoreUsageAverage: 0.56, cpuCoreLimitAverage: 1.6, cpuCost: 5.23,  cpuCostIdle: 0,  ramByteRequestAverage:  536870912, ramByteUsageAverage:  402653184, ramByteLimitAverage: 1073741824, ramCost: 4.01,  ramCostIdle: 0, efficiency: 0.70 },
          },
        }],
        window: win,
      },
    };
  }

  const namespace = namespaceOf(queryParams);

  let totalCPU = 0, totalRAM = 0;
  Object.values(costs.allocations).forEach((alloc) => {
    totalCPU += alloc.cpuCost;
    totalRAM += alloc.ramCost;
  });

  // Simulate realistic request/usage values for efficiency calculation
  let cpuCoreRequestAverage = 1.0;
  let cpuCoreUsageAverage = 0.4;
  let cpuCoreLimitAverage = 2.0;
  let ramByteRequestAverage = 536870912;   // 512 MB
  let ramByteUsageAverage = 375809638;     // ~358 MB
  let ramByteLimitAverage = 1073741824;
  let efficiency = 0.55;
  if (costs.live) {
    // The cluster's pods: their own requests and use, and an efficiency weighted by the cost of what
    // they requested but didn't use.
    const pods = Object.values(costs.allocations);
    const sum = (field) => pods.reduce((total, alloc) => total + alloc[field], 0);
    const unused = (cost, request, usage) => (request > usage ? cost * (1 - usage / request) : 0);
    cpuCoreRequestAverage = sum('cpuCoreRequestAverage');
    cpuCoreUsageAverage = sum('cpuCoreUsageAverage');
    cpuCoreLimitAverage = sum('cpuCoreLimitAverage');
    ramByteRequestAverage = sum('ramByteRequestAverage');
    ramByteUsageAverage = sum('ramByteUsageAverage');
    ramByteLimitAverage = sum('ramByteLimitAverage');
    const unusedCost = pods.reduce((total, a) =>
      total + unused(a.cpuCost, a.cpuCoreRequestAverage, a.cpuCoreUsageAverage)
        + unused(a.ramCost, a.ramByteRequestAverage, a.ramByteUsageAverage), 0);
    efficiency = totalCPU + totalRAM > 0 ? 1 - unusedCost / (totalCPU + totalRAM) : 0;
  }
  // Real OpenCost never computes its idle fields (up to 1.121.3 they are only summed, so always 0);
  // the service measures idle cost from the pods' requests and usage instead.
  const cpuCostIdle = 0;
  const ramCostIdle = 0;

  const window = { start: costs.window[0], end: costs.window[1] };
  return {
    code: 200,
    data: {
      step: 86400000000000,
      sets: [
        {
          window,
          allocations: {
            [namespace]: {
              cpuCoreRequestAverage,
              cpuCoreUsageAverage,
              cpuCoreLimitAverage,
              cpuCost: totalCPU,
              cpuCostIdle,
              ramByteRequestAverage,
              ramByteUsageAverage,
              ramByteLimitAverage,
              ramCost: totalRAM,
              ramCostIdle,
              networkCost: 0,
              loadBalancerCost: 0,
              pvCost: 0,
              sharedCost: 0,
              externalCost: 0,
              efficiency,
            },
          },
        },
      ],
      window,
    },
  };
}

// Handler for /health endpoint
function handleHealth() {
  return { status: 'ok', service: 'mock-opencost' };
}

// route answers one request.
async function route(req, res) {
  const parsedUrl = url.parse(req.url, true);
  const pathname = parsedUrl.pathname;
  const query = parsedUrl.query;

  // Enable CORS
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Methods', 'GET, OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type');
  res.setHeader('Content-Type', 'application/json');

  // Handle preflight requests
  if (req.method === 'OPTIONS') {
    res.writeHead(200);
    res.end();
    return;
  }

  // Route requests
  if (pathname === '/health') {
    const response = handleHealth();
    res.writeHead(200);
    res.end(JSON.stringify(response));
  } else if (pathname === '/allocation') {
    const response = handleAllocation(query, await costsFor(namespaceOf(query), query.window));
    res.writeHead(response.code || 200);
    res.end(JSON.stringify(response));
  } else if (pathname === '/allocation/autocomplete') {
    // Label keys pods carry, as OpenCost 1.120.3+ reports them (sanitized, so dots become underscores).
    res.writeHead(200);
    res.end(JSON.stringify({
      code: 200,
      data: { data: ['app', 'app_kubernetes_io_name', 'cost_center', 'env', 'team'] },
    }));
  } else if (pathname === '/allocation/summary') {
    const response = handleAllocationSummary(query, await costsFor(namespaceOf(query), query.window));
    res.writeHead(response.code || 200);
    res.end(JSON.stringify(response));
  } else if (pathname === '/assets') {
    // Simulate EKS control-plane fee: $72/mo ÷ 30 = $2.40/day
    res.writeHead(200);
    res.end(JSON.stringify({
      code: 200,
      data: [{
        ClusterManagement: {
          type: 'ClusterManagement',
          properties: { provider: 'AWS', cluster: 'mock-cluster' },
          totalCost: 2.4,
        },
      }],
    }));
  } else if (pathname === '/pricingSourceStatus') {
    res.writeHead(200);
    res.end(JSON.stringify({
      code: 200,
      data: 'Using default on-demand pricing. Connect cloud billing (AWS CUR / GCP Billing / Azure) for actuals.',
    }));
  } else if (pathname === '/orphanedPods') {
    const namespace = query.namespace || 'default';
    const costs = mockCosts[namespace] || mockCosts['default'];
    const orphaned = Object.entries(costs.allocations)
      .filter(([, alloc]) => alloc.cpuCoreUsageAverage === 0 && alloc.ramByteUsageAverage === 0 && alloc.totalCost > 0)
      .map(([key, alloc]) => ({
        name: key.replace('pod:', ''),
        namespace,
        controller: alloc.controller || '',
        controllerKind: alloc.controllerKind || '',
        cpuCost: 0,
        ramCost: 0,
        storageCost: (alloc.pvCost || 0) * 30,
        totalCost: alloc.totalCost * 30,
      }));
    res.writeHead(200);
    res.end(JSON.stringify({ orphanedPods: orphaned, totalWastedCost: orphaned.reduce((s, p) => s + p.totalCost, 0) }));
  } else {
    res.writeHead(404);
    res.end(JSON.stringify({ error: 'Not found' }));
  }

  // Log request
  console.log(`[${new Date().toISOString()}] ${req.method} ${req.url}`);
}

// Create HTTP server
const server = http.createServer((req, res) => {
  route(req, res).catch((err) => {
    console.error(`[${new Date().toISOString()}] ${req.method} ${req.url}: ${err.stack || err}`);
    if (!res.headersSent) res.writeHead(500);
    res.end(JSON.stringify({ code: 500, data: { error: String(err.message || err) } }));
  });
});

const PORT = process.env.PORT || 9003;
server.listen(PORT, '0.0.0.0', () => {
  console.log(`Mock OpenCost server listening on http://0.0.0.0:${PORT}`);
  console.log('Endpoints:');
  console.log(`  - GET /health`);
  console.log(IN_CLUSTER
    ? `  Serving the cluster's pods, prices x${PRICE_MULTIPLIER}`
    : '  Serving fixed pods (not in a cluster)');
  console.log(`  - GET /allocation?window=...&aggregate=namespace|pod|pod,container|controller&filter=namespace:"..."`)
});
