DROP INDEX IF EXISTS idx_cost_actuals_deployment_ns_measured;
CREATE INDEX IF NOT EXISTS idx_cost_actuals_namespace_measured
    ON cost_actuals (application_namespace, measured_at DESC);
