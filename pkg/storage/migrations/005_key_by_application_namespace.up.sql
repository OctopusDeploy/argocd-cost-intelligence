-- An Argo CD Application is identified by its namespace and name: with apps-in-any-namespace,
-- team-a:web and team-b:web are different Applications. Key per-application data by both.

-- Backfill: a row without an application namespace came from a notifications template that did
-- not send applicationNamespace, so which Application it belongs to is not recorded. When every
-- other row names the same namespace, the install has one Application namespace and the row
-- belongs to it. Otherwise it stays empty: no request can match it, and the application's next
-- sync writes a record that does. Guessing could show one team's costs to another.
UPDATE cost_actuals
SET application_namespace = (
    SELECT MIN(application_namespace) FROM cost_actuals WHERE application_namespace <> ''
)
WHERE application_namespace = ''
  AND (SELECT COUNT(DISTINCT application_namespace) FROM cost_actuals WHERE application_namespace <> '') = 1;

DROP INDEX IF EXISTS idx_cost_actuals_app_measured;
CREATE INDEX IF NOT EXISTS idx_cost_actuals_app_ref_measured
    ON cost_actuals (application_namespace, application_name, measured_at DESC);

ALTER TABLE cloud_estimates ADD COLUMN IF NOT EXISTS app_namespace TEXT NOT NULL DEFAULT '';

-- Backfill: an estimate was stored by app name only. It belongs to the namespace its app's cost
-- records name, when they name exactly one; failing that, to the install's only Application
-- namespace. Anything else is ambiguous and stays empty (unreachable) until CI pushes it again.
UPDATE cloud_estimates ce
SET app_namespace = ca.application_namespace
FROM (
    SELECT application_name, MIN(application_namespace) AS application_namespace
    FROM cost_actuals
    WHERE application_namespace <> ''
    GROUP BY application_name
    HAVING COUNT(DISTINCT application_namespace) = 1
) ca
WHERE ce.app_namespace = '' AND ce.app_name = ca.application_name;

UPDATE cloud_estimates
SET app_namespace = (
    SELECT MIN(application_namespace) FROM cost_actuals WHERE application_namespace <> ''
)
WHERE app_namespace = ''
  AND NOT EXISTS (
      SELECT 1 FROM cost_actuals
      WHERE application_name = cloud_estimates.app_name AND application_namespace <> ''
  )
  AND (SELECT COUNT(DISTINCT application_namespace) FROM cost_actuals WHERE application_namespace <> '') = 1;

-- New rows must name their namespace.
ALTER TABLE cloud_estimates ALTER COLUMN app_namespace DROP DEFAULT;
ALTER TABLE cloud_estimates DROP CONSTRAINT IF EXISTS cloud_estimates_pkey;
ALTER TABLE cloud_estimates ADD PRIMARY KEY (app_namespace, app_name, source);
