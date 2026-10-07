-- Each application's cost per UTC day, recorded once the day is over. OpenCost answers only for the
-- days its Prometheus still keeps (often 15), so without these rows a long range would be mostly
-- empty. cost_actuals cannot stand in: it gets a row per sync, and an app that does not sync for
-- weeks would have none. scope says whether a day measures the app's own pods ('application') or its
-- whole deployment namespace ('namespace'); the two are never shown together.
CREATE TABLE IF NOT EXISTS cost_daily (
    application_namespace VARCHAR(255)     NOT NULL,
    application_name      VARCHAR(255)     NOT NULL,
    day                   DATE             NOT NULL,
    deployment_namespace  VARCHAR(255)     NOT NULL,
    scope                 VARCHAR(16)      NOT NULL,
    total_cost            DOUBLE PRECISION NOT NULL,
    cpu_cost              DOUBLE PRECISION NOT NULL,
    memory_cost           DOUBLE PRECISION NOT NULL,
    network_cost          DOUBLE PRECISION NOT NULL,
    storage_cost          DOUBLE PRECISION NOT NULL,
    recorded_at           TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application_namespace, application_name, day)
);

-- Pruning deletes by day across every application.
CREATE INDEX IF NOT EXISTS idx_cost_daily_day ON cost_daily (day);

-- The last day recorded per application, and in which scope. A day OpenCost had no cost for leaves
-- no cost_daily row, so this is what tells a day already asked about from one not yet recorded.
CREATE TABLE IF NOT EXISTS cost_daily_progress (
    application_namespace VARCHAR(255) NOT NULL,
    application_name      VARCHAR(255) NOT NULL,
    recorded_through      DATE         NOT NULL,
    scope                 VARCHAR(16)  NOT NULL,
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application_namespace, application_name)
);
