-- +goose Up
CREATE TABLE idempotency_records (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    request_hash VARCHAR(64) NOT NULL,
    transfer_id BIGINT REFERENCES transfers(id) ON DELETE RESTRICT,
    status VARCHAR(20) NOT NULL DEFAULT 'PROCESSING',
    response_body JSONB,
    response_code INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_idempotency_status CHECK (status IN ('PROCESSING', 'COMPLETED', 'FAILED')),
    CONSTRAINT chk_request_hash CHECK (length(request_hash) = 64),
    CONSTRAINT chk_idempotency_response CHECK (
        status = 'PROCESSING'
        OR (response_body IS NOT NULL AND response_code IS NOT NULL)
    )
);

-- +goose Down
DROP TABLE IF EXISTS idempotency_records;
