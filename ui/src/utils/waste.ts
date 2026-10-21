import { AppWaste } from './api';
import { recommendCpuRequest } from './costInsights';

export interface WasteRow extends AppWaste {
  // share is the idle cost as a fraction of the application's own cost.
  share: number;
  // saving is the monthly saving of the lower CPU request the application's tab suggests, or null
  // when it suggests none.
  saving: number | null;
}

// WasteTotal sums the ranked applications.
export interface WasteTotal {
  count: number;
  idleCost: number;
  saving: number;
  // suggested is how many of them have a lower CPU request to suggest.
  suggested: number;
}

export interface WasteTable {
  rows: WasteRow[];
  total: WasteTotal;
}

// wasteRow prices an application's lower CPU request with the rule its own tab uses, from the same
// application-scope figures, so both pages show the same saving.
export function wasteRow(app: AppWaste): WasteRow {
  return {
    ...app,
    share: app.monthlyCost > 0 ? app.idleCost / app.monthlyCost : 0,
    saving: recommendCpuRequest(app.efficiency, app.cpuCost)?.monthlySavings ?? null,
  };
}

// wasteTable lays out ranked applications and their total, which the rows add up to.
export function wasteTable(apps: AppWaste[]): WasteTable {
  const rows = apps.map(wasteRow);
  return {
    rows,
    total: {
      count: rows.length,
      idleCost: rows.reduce((total, row) => total + row.idleCost, 0),
      saving: rows.reduce((total, row) => total + (row.saving ?? 0), 0),
      suggested: rows.filter(row => row.saving !== null).length,
    },
  };
}

// deploymentNamespaces lists the namespaces ranked applications deploy to, for the filter.
export function deploymentNamespaces(apps: AppWaste[]): string[] {
  return Array.from(new Set(apps.map(app => app.deploymentNamespace))).sort();
}

// notRankedDetail says why applications are left out of the ranking, or null when none are.
export function notRankedDetail(namespaceOnly: number, noPods: number): string | null {
  const parts: string[] = [];
  if (namespaceOnly > 0) {
    parts.push(
      namespaceOnly === 1
        ? '1 measured only as its whole namespace, until its next sync'
        : `${namespaceOnly} measured only as their whole namespace, until their next sync`
    );
  }
  if (noPods > 0) parts.push(`${noPods} with no pods`);
  return parts.length > 0 ? parts.join(', and ') : null;
}
