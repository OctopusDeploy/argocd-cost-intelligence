-- measured_on says what took a measurement: 'sync' for the sync webhook, 'install' for the startup
-- backfill, which measures applications that have no record yet. An install measurement follows no
-- sync and is never the baseline of a change since the last sync. Its sync_timestamp holds its
-- measurement time, so the column stays NOT NULL and older replicas can still read it.
ALTER TABLE cost_actuals ADD COLUMN IF NOT EXISTS measured_on VARCHAR(16) NOT NULL DEFAULT 'sync';
