-- +goose Up
CREATE TABLE wallets (
    id VARCHAR(43) PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_wallet_balance CHECK (balance >= 0)
);

-- +goose Down
DROP TABLE IF EXISTS wallets;
