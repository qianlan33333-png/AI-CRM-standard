-- Open-only client progress. No job, message intent or external effect is created.
CREATE TABLE group_ops_navigation_runs (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 staff_id BIGINT NOT NULL REFERENCES admin_users(id),
 plan_id BIGINT NOT NULL REFERENCES group_ops_plans(id),
 key_digest BYTEA NOT NULL CHECK(octet_length(key_digest)=32),
 plan_revision BIGINT NOT NULL CHECK(plan_revision>0),
 revision BIGINT NOT NULL CHECK(revision>0),
 state TEXT NOT NULL CHECK(state IN ('paused','waiting','open_requested','completed','cancelled')),
 snapshot JSONB NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 created_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL,
 UNIQUE(staff_id,key_digest)
);
CREATE UNIQUE INDEX group_ops_navigation_one_active ON group_ops_navigation_runs(staff_id)
 WHERE state NOT IN ('completed','cancelled');
CREATE INDEX group_ops_navigation_recent ON group_ops_navigation_runs(staff_id,id DESC);
