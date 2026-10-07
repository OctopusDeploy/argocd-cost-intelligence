ALTER TABLE cost_actuals ADD COLUMN IF NOT EXISTS efficiency_json JSONB;
ALTER TABLE cost_actuals ADD COLUMN IF NOT EXISTS pods_json       JSONB;
