-- Qualification scope belongs to Referral; settlement snapshots belong to Distribution.
ALTER TABLE referral_campaigns ADD COLUMN compatible_product_ids bigint[] NOT NULL DEFAULT '{}';
