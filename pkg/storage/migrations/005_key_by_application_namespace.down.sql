-- Keying by name alone again merges same-named Applications from different namespaces. Keep each
-- (app_name, source)'s most recent estimate so the old primary key can be restored.
DELETE FROM cloud_estimates ce
USING cloud_estimates newer
WHERE ce.app_name = newer.app_name
  AND ce.source = newer.source
  AND (newer.pushed_at, newer.app_namespace) > (ce.pushed_at, ce.app_namespace);

ALTER TABLE cloud_estimates DROP CONSTRAINT IF EXISTS cloud_estimates_pkey;
ALTER TABLE cloud_estimates DROP COLUMN IF EXISTS app_namespace;
ALTER TABLE cloud_estimates ADD PRIMARY KEY (app_name, source);

DROP INDEX IF EXISTS idx_cost_actuals_app_ref_measured;
CREATE INDEX IF NOT EXISTS idx_cost_actuals_app_measured
    ON cost_actuals (application_name, measured_at DESC);
