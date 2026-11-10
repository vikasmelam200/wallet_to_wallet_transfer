-- +goose Up
CREATE TABLE transfers (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    from_wallet_id VARCHAR(43) NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    to_wallet_id VARCHAR(43) NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    amount BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_transfer_amount CHECK (amount > 0),
    CONSTRAINT chk_transfer_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT chk_transfer_status CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

-- +goose Down
DROP TABLE IF EXISTS transfers;
