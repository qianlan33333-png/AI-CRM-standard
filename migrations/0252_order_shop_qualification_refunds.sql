ALTER TABLE order_wechat_shop_facts ADD COLUMN qualification_refund_state text NOT NULL DEFAULT 'unknown' CHECK (qualification_refund_state IN ('unknown','clear','pending','refunded'));
ALTER TABLE order_wechat_shop_facts ADD COLUMN qualification_checked_at timestamptz;
