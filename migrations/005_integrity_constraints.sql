-- +goose Up

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id, created_at DESC);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id, created_at DESC);
CREATE INDEX idx_ledger_wallet ON ledger_entries(wallet_id, created_at DESC);
CREATE INDEX idx_ledger_transfer ON ledger_entries(transfer_id);
CREATE INDEX idx_idempotency_transfer ON idempotency_records(transfer_id);

CREATE OR REPLACE FUNCTION check_transfer_ledger()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    v_transfer_id BIGINT;
    v_status VARCHAR(20);
    v_from_wallet VARCHAR(43);
    v_to_wallet VARCHAR(43);
    v_amount BIGINT;
    v_count BIGINT;
    v_debit_count BIGINT;
    v_credit_count BIGINT;
    v_debit_wallet VARCHAR(43);
    v_credit_wallet VARCHAR(43);
    v_debit_amount BIGINT;
    v_credit_amount BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'transfers' THEN
        v_transfer_id := NEW.id;
    ELSE
        v_transfer_id := NEW.transfer_id;
    END IF;

    SELECT status, from_wallet_id, to_wallet_id, amount
    INTO v_status, v_from_wallet, v_to_wallet, v_amount
    FROM transfers
    WHERE id = v_transfer_id;

    IF NOT FOUND THEN
        RETURN NULL;
    END IF;

    SELECT
        COUNT(*),
        COUNT(*) FILTER (WHERE type = 'DEBIT'),
        COUNT(*) FILTER (WHERE type = 'CREDIT'),
        MAX(wallet_id) FILTER (WHERE type = 'DEBIT'),
        MAX(wallet_id) FILTER (WHERE type = 'CREDIT'),
        MAX(amount) FILTER (WHERE type = 'DEBIT'),
        MAX(amount) FILTER (WHERE type = 'CREDIT')
    INTO
        v_count, v_debit_count, v_credit_count,
        v_debit_wallet, v_credit_wallet,
        v_debit_amount, v_credit_amount
    FROM ledger_entries
    WHERE transfer_id = v_transfer_id;

    IF v_status = 'PROCESSED' THEN
        IF v_count <> 2
            OR v_debit_count <> 1
            OR v_credit_count <> 1
            OR v_debit_wallet IS DISTINCT FROM v_from_wallet
            OR v_credit_wallet IS DISTINCT FROM v_to_wallet
            OR v_debit_amount IS DISTINCT FROM v_amount
            OR v_credit_amount IS DISTINCT FROM v_amount
        THEN
            RAISE EXCEPTION 'Invalid ledger entries for transfer %', v_transfer_id;
        END IF;
    ELSE
        IF v_count <> 0 THEN
            RAISE EXCEPTION 'Non-processed transfer % cannot have ledger entries', v_transfer_id;
        END IF;
    END IF;

    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER trg_transfer_ledger_check
AFTER INSERT OR UPDATE ON transfers
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION check_transfer_ledger();

CREATE CONSTRAINT TRIGGER trg_ledger_entry_check
AFTER INSERT ON ledger_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION check_transfer_ledger();

CREATE OR REPLACE FUNCTION prevent_ledger_modification()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'Ledger entries are immutable. Create reversal entries instead.';
END;
$$;

CREATE TRIGGER trg_ledger_immutable
BEFORE UPDATE OR DELETE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_ledger_modification();

CREATE OR REPLACE FUNCTION enforce_transfer_state()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.from_wallet_id IS DISTINCT FROM NEW.from_wallet_id
        OR OLD.to_wallet_id IS DISTINCT FROM NEW.to_wallet_id
        OR OLD.amount IS DISTINCT FROM NEW.amount
    THEN
        RAISE EXCEPTION 'Transfer details cannot be modified';
    END IF;

    IF OLD.status IN ('PROCESSED', 'FAILED') AND NEW.status <> OLD.status THEN
        RAISE EXCEPTION 'Terminal transfer state cannot be changed';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_transfer_state
BEFORE UPDATE ON transfers
FOR EACH ROW
EXECUTE FUNCTION enforce_transfer_state();

-- +goose Down
DROP TRIGGER IF EXISTS trg_transfer_state ON transfers;
DROP FUNCTION IF EXISTS enforce_transfer_state();
DROP TRIGGER IF EXISTS trg_ledger_immutable ON ledger_entries;
DROP FUNCTION IF EXISTS prevent_ledger_modification();
DROP TRIGGER IF EXISTS trg_ledger_entry_check ON ledger_entries;
DROP TRIGGER IF EXISTS trg_transfer_ledger_check ON transfers;
DROP FUNCTION IF EXISTS check_transfer_ledger();
DROP INDEX IF EXISTS idx_idempotency_transfer;
DROP INDEX IF EXISTS idx_ledger_transfer;
DROP INDEX IF EXISTS idx_ledger_wallet;
DROP INDEX IF EXISTS idx_transfers_to_wallet;
DROP INDEX IF EXISTS idx_transfers_from_wallet;
