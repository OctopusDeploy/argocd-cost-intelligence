-- Every record measures the application's whole deployment namespace, which other applications and
-- workloads may share. app_json holds the application's own share of that measurement: the pods its
-- controllers own, with their cost, breakdown and efficiency, and the controllers themselves. NULL for
-- records stored before it existed or synced without the application's resources.
ALTER TABLE cost_actuals ADD COLUMN IF NOT EXISTS app_json JSONB;
