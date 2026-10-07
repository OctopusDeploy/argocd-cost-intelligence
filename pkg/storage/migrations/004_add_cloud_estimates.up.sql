-- Cloud service cost estimates pushed by IaC cost tools (Infracost, Terracost, …).
-- One row per (app_name, source) — upsert on push so each tool keeps its latest estimate.
CREATE TABLE IF NOT EXISTS cloud_estimates (
    app_name  TEXT        NOT NULL,
    source    TEXT        NOT NULL,
    total_monthly_cost NUMERIC(12,4) NOT NULL DEFAULT 0,
    currency  TEXT        NOT NULL DEFAULT 'USD',
    resources_json JSONB,
    pushed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (app_name, source)
);
