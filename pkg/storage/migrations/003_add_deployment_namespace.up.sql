ALTER TABLE cost_actuals ADD COLUMN IF NOT EXISTS deployment_namespace TEXT NOT NULL DEFAULT '';
