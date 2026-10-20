-- idle_monthly_cost is the namespace's CPU and memory cost paid for requests its pods did not use,
-- measured from the pods. OpenCost's own idle fields are always zero, so records measured before this
-- column have no idle cost: it stays NULL for them, and for any record an older replica writes during a
-- rolling upgrade, rather than reading as zero.
ALTER TABLE cost_actuals ADD COLUMN IF NOT EXISTS idle_monthly_cost NUMERIC(12,4);
