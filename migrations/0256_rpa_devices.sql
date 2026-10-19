ALTER TABLE admin_sessions ADD COLUMN authentication_method text NOT NULL DEFAULT 'legacy'
 CHECK(authentication_method IN ('legacy','local_password','cli_password','wecom_oauth'));
CREATE TABLE access_rpa_devices (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 token_digest bytea NOT NULL UNIQUE CHECK(octet_length(token_digest)=32),
 user_code_digest bytea NOT NULL UNIQUE CHECK(octet_length(user_code_digest)=32),
 name text NOT NULL CHECK(length(name)>0),
 staff_id bigint REFERENCES admin_users(id),
 sender_userid text,
 session_version bigint,
 state text NOT NULL CHECK(state IN ('pending','active','revoked')),
 pair_expires_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 CHECK(state<>'active' OR (staff_id IS NOT NULL AND sender_userid IS NOT NULL AND session_version IS NOT NULL))
);
ALTER TABLE admin_access_audit DROP CONSTRAINT ck_admin_access_audit_action;
ALTER TABLE admin_access_audit ADD CONSTRAINT ck_admin_access_audit_action CHECK (
 action IN ('bootstrap','create','disable','bind_wecom_userid','change_roles','reset_password','set_login_enabled',
 'provision_admin','provision_viewer','enable_login','disable_login','set_role','transfer_super_admin',
 'refresh_staff_name','cli_policy_changed','access.rpa_device_approved','access.rpa_device_revoked')
);
