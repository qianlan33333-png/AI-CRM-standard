-- Owner: internal/access. Existing browser sessions retain their expiry.
CREATE TABLE admin_cli_policies (
    admin_user_id BIGINT PRIMARY KEY REFERENCES admin_users(id),
    allowed_cidrs TEXT[] NOT NULL DEFAULT '{}',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0)
);
ALTER TABLE admin_sessions ADD COLUMN credential_kind TEXT NOT NULL DEFAULT 'browser'
    CHECK (credential_kind IN ('browser', 'cli'));
ALTER TABLE admin_sessions ADD COLUMN cli_policy_version BIGINT NOT NULL DEFAULT 1
    CHECK (cli_policy_version > 0);
ALTER TABLE admin_sessions ALTER COLUMN expires_at DROP NOT NULL;
ALTER TABLE admin_sessions DROP CONSTRAINT ck_admin_sessions_expiry;
ALTER TABLE admin_sessions ADD CONSTRAINT ck_admin_sessions_expiry CHECK (
    (expires_at IS NOT NULL AND expires_at > created_at)
    OR (credential_kind = 'cli' AND expires_at IS NULL)
);

ALTER TABLE admin_access_audit DROP CONSTRAINT ck_admin_access_audit_action;
ALTER TABLE admin_access_audit ADD CONSTRAINT ck_admin_access_audit_action CHECK (
 action IN ('bootstrap','create','disable','bind_wecom_userid','change_roles','reset_password','set_login_enabled',
 'provision_admin','provision_viewer','enable_login','disable_login','set_role','transfer_super_admin',
 'refresh_staff_name','cli_policy_changed')
);
