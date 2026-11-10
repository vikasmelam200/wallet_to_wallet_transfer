-- +goose Up
CREATE TABLE ledger_entries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    wallet_id VARCHAR(43) NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    transfer_id BIGINT NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    type VARCHAR(10) NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_ledger_type CHECK (type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT chk_ledger_amount CHECK (amount > 0)
);

-- +goose Down
DROP TABLE IF EXISTS ledger_entries;
