-- Customer owns derived text context. A staff scope is never interchangeable
-- with a customer-wide authorized operations scope (staff_id=0).
CREATE TABLE customer_contexts (
 customer_id bigint NOT NULL REFERENCES customers(id),
 staff_id bigint NOT NULL CHECK(staff_id >= 0),
 source_version text NOT NULL,
 projection jsonb NOT NULL,
 prepared_at timestamptz NOT NULL,
 PRIMARY KEY(customer_id,staff_id)
);

-- Product owns private operating notes; they never enter public Product DTOs.
CREATE TABLE product_operating_notes (
 product_id bigint PRIMARY KEY REFERENCES products(id),
 notes text NOT NULL DEFAULT '',
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 updated_by bigint NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
);

-- Automation owns auxiliary context/reply generations. Execution and recovery
-- use the existing External Effects / River worker, never a second queue.
CREATE TABLE automation_auxiliary_generations (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 request_digest text NOT NULL UNIQUE,
 purpose text NOT NULL CHECK(purpose IN ('context','sidebar_reply')),
 customer_id bigint NOT NULL,
 staff_id bigint NOT NULL CHECK(staff_id>=0),
 source_version text NOT NULL,
 dispatch jsonb NOT NULL,
 state text NOT NULL,
 effect_id text UNIQUE,
 result_text text NOT NULL DEFAULT '',
 failure_code text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 completed_at timestamptz
);

-- A frozen existing ConfirmRun command per intended schedule occurrence.
-- River owns scheduling/recovery; this is an idempotency receipt, not a queue.
CREATE TABLE automation_cycle_generations (
 occurrence text PRIMARY KEY,
 command jsonb NOT NULL,
 run_id bigint,
 created_at timestamptz NOT NULL DEFAULT now()
);
