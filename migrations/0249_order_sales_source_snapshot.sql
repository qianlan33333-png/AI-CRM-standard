ALTER TABLE order_checkout_snapshots ADD COLUMN sales_source_kind TEXT NOT NULL DEFAULT '', ADD COLUMN sales_source_id BIGINT, ADD COLUMN sales_source_name TEXT NOT NULL DEFAULT '',
 ADD CONSTRAINT order_checkout_sales_source_shape CHECK (
 (sales_source_kind IN ('','standard','unrecorded') AND sales_source_id IS NULL AND sales_source_name='') OR
 (sales_source_kind='channel' AND sales_source_id IS NOT NULL AND sales_source_id>0 AND char_length(sales_source_name)>0 AND octet_length(sales_source_name)<=500)
 );
-- Immutable Order snapshots retain historical IDs, without a cross-domain FK.
CREATE INDEX order_checkout_sales_source_idx ON order_checkout_snapshots(sales_source_id,order_id) WHERE sales_source_id IS NOT NULL;
