-- Atomic task accounting is independent of request transactions: failed work
-- still consumes budget. owner_id=0 denotes the shared system/global bucket.
CREATE TABLE IF NOT EXISTS task_budget_daily (
 owner_id INT NOT NULL CHECK(owner_id>=0), bucket TEXT NOT NULL,
 day DATE NOT NULL, used INT NOT NULL CHECK(used>=0),
 PRIMARY KEY(owner_id,bucket,day)
);
CREATE TABLE IF NOT EXISTS task_budget_leases (
 id TEXT PRIMARY KEY, owner_id INT NOT NULL CHECK(owner_id>=0),
 bucket TEXT NOT NULL, expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS task_budget_leases_active ON task_budget_leases(bucket,owner_id,expires_at);
ALTER TABLE task_budget_daily ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_budget_daily FORCE ROW LEVEL SECURITY;
ALTER TABLE task_budget_leases ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_budget_leases FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS task_budget_daily_owner ON task_budget_daily;
CREATE POLICY task_budget_daily_owner ON task_budget_daily USING(app_rls_bypass() OR owner_id=app_current_user_id()) WITH CHECK(app_rls_bypass() OR owner_id=app_current_user_id());
DROP POLICY IF EXISTS task_budget_leases_owner ON task_budget_leases;
CREATE POLICY task_budget_leases_owner ON task_budget_leases USING(app_rls_bypass() OR owner_id=app_current_user_id()) WITH CHECK(app_rls_bypass() OR owner_id=app_current_user_id());
ALTER TABLE user_insights ADD COLUMN IF NOT EXISTS requested_at TIMESTAMPTZ;
UPDATE user_insights SET requested_at=generated_at WHERE requested_at IS NULL;
ALTER TABLE user_insights ALTER COLUMN requested_at SET DEFAULT NOW();
ALTER TABLE user_insights ALTER COLUMN requested_at SET NOT NULL;
CREATE INDEX IF NOT EXISTS user_insights_manual_budget ON user_insights(user_id,requested_at) WHERE triggered_by='manual';
