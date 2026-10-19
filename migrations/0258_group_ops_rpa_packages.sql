-- One immutable Markdown configuration and its existing frozen execution.
CREATE TABLE group_ops_rpa_packages (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 plan_id bigint NOT NULL REFERENCES group_ops_plans(id),
 staff_id bigint NOT NULL,
 key_digest bytea NOT NULL CHECK(octet_length(key_digest)=32),
 markdown text NOT NULL,
 sha256 text NOT NULL CHECK(length(sha256)=64),
 run_id bigint NOT NULL UNIQUE REFERENCES group_ops_navigation_runs(id),
 created_at timestamptz NOT NULL,
 UNIQUE(staff_id,key_digest)
);
CREATE INDEX group_ops_rpa_packages_plan_latest ON group_ops_rpa_packages(plan_id,id DESC);
