CREATE TABLE distribution_attribution_purchase_scopes (
 attribution_id bigint PRIMARY KEY REFERENCES distribution_order_attributions(id),
 campaign_id bigint NOT NULL CHECK(campaign_id>0), campaign_version bigint NOT NULL CHECK(campaign_version>0),
 compatible_product_ids bigint[] NOT NULL, created_at timestamptz NOT NULL
);

CREATE TRIGGER distribution_purchase_scopes_append_only BEFORE UPDATE OR DELETE OR TRUNCATE ON distribution_attribution_purchase_scopes FOR EACH STATEMENT EXECUTE FUNCTION distribution_append_only_reject_mutation();
