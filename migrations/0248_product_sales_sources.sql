CREATE TABLE product_sales_sources (
 id BIGSERIAL PRIMARY KEY, product_id BIGINT NOT NULL REFERENCES products(id),
 name TEXT NOT NULL CHECK (char_length(name)>0 AND octet_length(name)<=500),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0), created_by BIGINT NOT NULL, updated_by BIGINT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX product_sales_sources_product_idx ON product_sales_sources(product_id,id);
