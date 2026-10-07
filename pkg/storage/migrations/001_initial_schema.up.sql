CREATE TABLE IF NOT EXISTS cost_actuals (
    id                    VARCHAR(255) PRIMARY KEY,
    application_name      VARCHAR(255) NOT NULL,
    application_namespace VARCHAR(255) NOT NULL,
    sync_timestamp        TIMESTAMP    NOT NULL,
    measured_at           TIMESTAMP    NOT NULL,
    total_monthly_cost    DECIMAL(10,2) NOT NULL,
    estimate_accuracy     DECIMAL(5,2),
    estimate_vs_actual    DECIMAL(10,2),
    resources_json        JSONB,
    opencost_output_json  JSONB,
    created_at            TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cost_actuals_app_measured
    ON cost_actuals (application_name, measured_at DESC);

CREATE INDEX IF NOT EXISTS idx_cost_actuals_namespace_measured
    ON cost_actuals (application_namespace, measured_at DESC);
