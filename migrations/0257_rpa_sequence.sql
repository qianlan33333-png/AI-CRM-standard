-- Local execution configuration extends an existing plan, with the same revision fence.
ALTER TABLE group_ops_operation_receipts DROP CONSTRAINT group_ops_operation_receipts_operation_check;
ALTER TABLE group_ops_operation_receipts ADD CONSTRAINT group_ops_operation_receipts_operation_check CHECK (operation IN (
 'plan_create','plan_update','plan_activate','plan_pause','plan_archive','member_add','member_remove','group_asset_add','group_asset_remove',
 'node_add','node_update','node_remove','webhook_descriptor_put','runtime_run_due','runtime_broadcast','runtime_webhook',
 'directory_groups_refresh','directory_members_refresh','execution_reconcile','execution_delivery_read','rpa_sequence'
));
CREATE TABLE group_ops_rpa_sequences (
 plan_id bigint PRIMARY KEY REFERENCES group_ops_plans(id),
 messages jsonb NOT NULL CHECK(jsonb_typeof(messages)='array'),
 updated_at timestamptz NOT NULL
);
ALTER TABLE group_ops_navigation_runs DROP CONSTRAINT group_ops_navigation_runs_state_check;
ALTER TABLE group_ops_navigation_runs ADD CONSTRAINT group_ops_navigation_runs_state_check
 CHECK(state IN ('paused','waiting','open_requested','verified','granted','outcome_unknown','final_failed','completed','cancelled'));
CREATE TABLE outbound_rpa_intents (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 run_ref bigint NOT NULL,
 group_index integer NOT NULL CHECK(group_index>=0),
 message_index integer NOT NULL CHECK(message_index>=0),
 device_id bigint NOT NULL,
 staff_id bigint NOT NULL,
 intent_digest bytea NOT NULL CHECK(octet_length(intent_digest)=32),
 payload jsonb NOT NULL,
 state text NOT NULL CHECK(state IN ('accepted','queued','client_executed','outcome_unknown','final_failed')),
 effect_id text UNIQUE,
 grant_digest bytea NOT NULL CHECK(octet_length(grant_digest)=32),
 evidence_digest bytea,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 UNIQUE(run_ref,group_index,message_index)
);
CREATE TABLE outbound_rpa_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 intent_id bigint NOT NULL REFERENCES outbound_rpa_intents(id),
 operation text NOT NULL,
 payload_digest bytea NOT NULL,
 occurred_at timestamptz NOT NULL
);
CREATE TABLE outbound_rpa_outbox (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 intent_id bigint NOT NULL REFERENCES outbound_rpa_intents(id),
 event_type text NOT NULL,
 payload jsonb NOT NULL,
 occurred_at timestamptz NOT NULL
);
ALTER TABLE external_effects DROP CONSTRAINT IF EXISTS external_effects_owner_kind_shape;
ALTER TABLE external_effects DROP CONSTRAINT IF EXISTS external_effects_kind_check;
ALTER TABLE external_effects ADD CONSTRAINT external_effects_kind_check CHECK (kind IN (
  'outbound_message','outbound_message_control','automation_message','outbound_media','wecom_tag_catalog','wecom_tag_catalog_mutation','wecom_contact_description','group_message',
  'channel_acquisition_asset','channel_welcome_message','channel_entry_tag',
  'group_invitation_code','channel_acquisition_link_mutation','sidebar_jssdk_send','rpa_client_send','survey_completion',
  'customer_owner_handoff','customer_tag_command','commerce_product_push','ai_agent_generate','ai_audience_recommend',
  'wechat_pay_prepay_v1','wechat_pay_refund_v1','wechat_shop_refund_v1','alipay_wap_pay_v1','alipay_page_pay_v1','alipay_refund_v1',
  'wechat_pay_profit_sharing_receiver_v1','wechat_pay_profit_sharing_order_v1','wechat_pay_profit_sharing_unfreeze_v1',
  'feishu_ops_notification_v1','wechat_invoice_issue_v1','wechat_invoice_reverse_v1','wechat_invoice_card_configure_v1'
));
ALTER TABLE external_effects ADD CONSTRAINT external_effects_owner_kind_shape CHECK (
  (owner='outbound' AND kind IN (
    'outbound_message','outbound_message_control','automation_message','outbound_media','wecom_tag_catalog','wecom_tag_catalog_mutation','wecom_contact_description','group_message',
    'channel_acquisition_asset','channel_welcome_message','channel_entry_tag',
    'group_invitation_code','channel_acquisition_link_mutation','sidebar_jssdk_send','rpa_client_send','survey_completion',
    'customer_owner_handoff','customer_tag_command','commerce_product_push'
  )) OR
  (owner='payment' AND kind IN (
    'wechat_pay_prepay_v1','wechat_pay_refund_v1','wechat_shop_refund_v1','alipay_wap_pay_v1','alipay_page_pay_v1','alipay_refund_v1',
    'wechat_pay_profit_sharing_receiver_v1','wechat_pay_profit_sharing_order_v1','wechat_pay_profit_sharing_unfreeze_v1'
  )) OR
  (owner='invoice' AND kind IN ('wechat_invoice_issue_v1','wechat_invoice_reverse_v1','wechat_invoice_card_configure_v1')) OR
  (owner='automation' AND kind='ai_agent_generate') OR (owner='adminops' AND kind='feishu_ops_notification_v1') OR (owner='segment' AND kind='ai_audience_recommend')
);
