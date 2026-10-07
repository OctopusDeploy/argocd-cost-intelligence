#!/usr/bin/env node

/**
 * Mock OpenCost Server for Local Testing
 *
 * Simulates the OpenCost API endpoints for local development and testing
 * without needing a real Kubernetes cluster with metrics.
 *
 * Provides fake but realistic cost data for testing cost-intelligence backend.
 */

const http = require('http');
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

// Handler for /allocation endpoint
function handleAllocation(queryParams) {
  const namespace = queryParams.filter ? queryParams.filter.replace('namespace:"', '').replace('"', '') : 'default';
  const aggregate = queryParams.aggregate || 'namespace';

  const costs = mockCosts[namespace] || mockCosts['default'];

  // Build response based on aggregation type
  if (aggregate === 'namespace') {
    // Daily time-series mode: step=1d
    if (queryParams.step === '1d') {

      let baseCPU = 0, baseRAM = 0, baseNetwork = 0, baseLB = 0, basePV = 0;
      Object.values(costs.allocations).forEach((alloc) => {
        baseCPU += alloc.cpuCost;
        baseRAM += alloc.ramCost;
        baseNetwork += alloc.networkCost || 0;
        baseLB += alloc.loadBalancerCost || 0;
        basePV += alloc.pvCost || 0;
      });
      // Scale from monthly to daily
      baseCPU /= 30; baseRAM /= 30; baseNetwork /= 30; baseLB /= 30; basePV /= 30;

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

    let totalCPU = 0;
    let totalRAM = 0;

    Object.values(costs.allocations).forEach((alloc) => {
      totalCPU += alloc.cpuCost;
      totalRAM += alloc.ramCost;
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
            cpuCoreHours: 3.5,
            cpuCost: totalCPU,
            ramByteHours: 3758096384,
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
            rawAllocationOnly: {
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
    Object.values(costs.allocations).forEach((alloc) => {
      if (!alloc.controller) return;
      const kind = (alloc.controllerKind || 'Deployment').toLowerCase();
      const key = `${kind}:${alloc.controller}`;
      const entry = byController[key] || (byController[key] = { kind, name: alloc.controller, cpu: 0, ram: 0, network: 0, pv: 0 });
      entry.cpu += alloc.cpuCost / 30;
      entry.ram += alloc.ramCost / 30;
      entry.network += (alloc.networkCost || 0) / 30;
      entry.pv += (alloc.pvCost || 0) / 30;
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
    Object.entries(costs.allocations).forEach(([key, alloc]) => {
      const podName = key.replace('pod:', '');
      const cpuUsage = alloc.cpuCoreUsageAverage ?? (alloc.cpuCoreHours > 0 ? alloc.cpuCoreHours / 24 : 0);
      const ramUsage = alloc.ramByteUsageAverage ?? (alloc.ramByteHours > 0 ? alloc.ramByteHours / 24 : 0);
      pods[podName] = {
        ...alloc,
        minutes: 24 * 60,
        cpuCoreUsageAverage: cpuUsage,
        ramByteUsageAverage: ramUsage,
        // Requests above use, so an application's efficiency and idle spend have something to show.
        cpuCoreRequestAverage: alloc.cpuCoreRequestAverage ?? cpuUsage * 2.5,
        ramByteRequestAverage: alloc.ramByteRequestAverage ?? ramUsage * 1.4,
        properties: {
          pod: podName,
          namespace,
          controllerKind: alloc.controllerKind || 'Deployment',
          controller: alloc.controller || podName,
        },
      };
    });

    return {
      code: 200,
      data: [pods],
    };
  }

  return { code: 400, data: { error: 'Invalid aggregation type' } };
}

// Handler for /allocation/summary endpoint
function handleAllocationSummary(queryParams) {
  const aggregate = queryParams.aggregate || 'namespace';

  if (aggregate.startsWith('label:')) {
    const win = { start: toRFC3339(new Date(Date.now() - 86400000)), end: toRFC3339(new Date()) };
    return {
      code: 200,
      data: {
        step: 86400000000000,
        sets: [{
          window: win,
          allocations: {
            'platform':         { cpuCoreRequestAverage: 2.0, cpuCoreUsageAverage: 1.24, cpuCoreLimitAverage: 4.0, cpuCost: 20.67, cpuCostIdle: 12.40, ramByteRequestAverage: 2147483648, ramByteUsageAverage: 1342177280, ramByteLimitAverage: 4294967296, ramCost: 16.01, ramCostIdle: 6.08, efficiency: 0.62 },
            'payments':         { cpuCoreRequestAverage: 1.5, cpuCoreUsageAverage: 1.07, cpuCoreLimitAverage: 3.0, cpuCost: 14.83, cpuCostIdle: 8.92,  ramByteRequestAverage: 1073741824, ramByteUsageAverage:  805306368, ramByteLimitAverage: 2147483648, ramCost: 11.01, ramCostIdle: 4.18, efficiency: 0.71 },
            'data-engineering': { cpuCoreRequestAverage: 1.0, cpuCoreUsageAverage: 0.55, cpuCoreLimitAverage: 2.0, cpuCost: 9.35,  cpuCostIdle: 5.61,  ramByteRequestAverage:  536870912, ramByteUsageAverage:  268435456, ramByteLimitAverage: 1073741824, ramCost: 7.34,  ramCostIdle: 2.94, efficiency: 0.55 },
            'infrastructure':   { cpuCoreRequestAverage: 0.8, cpuCoreUsageAverage: 0.56, cpuCoreLimitAverage: 1.6, cpuCost: 5.23,  cpuCostIdle: 3.14,  ramByteRequestAverage:  536870912, ramByteUsageAverage:  402653184, ramByteLimitAverage: 1073741824, ramCost: 4.01,  ramCostIdle: 1.60, efficiency: 0.70 },
          },
        }],
        window: win,
      },
    };
  }

  const namespace = queryParams.filter ? queryParams.filter.replace('namespace:"', '').replace('"', '') : 'default';
  const costs = mockCosts[namespace] || mockCosts['default'];

  let totalCPU = 0, totalRAM = 0;
  Object.values(costs.allocations).forEach((alloc) => {
    totalCPU += alloc.cpuCost;
    totalRAM += alloc.ramCost;
  });

  // Simulate realistic request/usage values for efficiency calculation
  const cpuCoreRequestAverage = 1.0;
  const cpuCoreUsageAverage = 0.4;
  const ramByteRequestAverage = 536870912;   // 512 MB
  const ramByteUsageAverage = 375809638;     // ~358 MB
  const cpuCostIdle = totalCPU * 0.60;      // 60% idle (40% efficient)
  const ramCostIdle = totalRAM * 0.30;      // 30% idle (70% efficient)

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
              cpuCoreLimitAverage: 2.0,
              cpuCost: totalCPU,
              cpuCostIdle,
              ramByteRequestAverage,
              ramByteUsageAverage,
              ramByteLimitAverage: 1073741824,
              ramCost: totalRAM,
              ramCostIdle,
              networkCost: 0,
              loadBalancerCost: 0,
              pvCost: 0,
              sharedCost: 0,
              externalCost: 0,
              efficiency: 0.55,
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

// Create HTTP server
const server = http.createServer((req, res) => {
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
    const response = handleAllocation(query);
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
    const response = handleAllocationSummary(query);
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
});

const PORT = process.env.PORT || 9003;
server.listen(PORT, '0.0.0.0', () => {
  console.log(`Mock OpenCost server listening on http://0.0.0.0:${PORT}`);
  console.log('Endpoints:');
  console.log(`  - GET /health`);
  console.log(`  - GET /allocation?window=...&aggregate=namespace|pod|controller&filter=namespace:"..."`)
});
