-- The namespace summary looks records up by the namespace an app deploys to. The old index on
-- (application_namespace, measured_at) served only that query when it wrongly filtered by where the
-- Application resource lives; per-application lookups use idx_cost_actuals_app_ref_measured.
DROP INDEX IF EXISTS idx_cost_actuals_namespace_measured;
CREATE INDEX IF NOT EXISTS idx_cost_actuals_deployment_ns_measured
    ON cost_actuals (deployment_namespace, measured_at DESC);
