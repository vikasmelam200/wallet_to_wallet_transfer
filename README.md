# Wallet Transfer Service

A small Gin + GORM + PostgreSQL wallet transfer API demonstrating transactional transfers, idempotency, double-entry ledger entries, and concurrency control.

## Architecture

```text
wallet-service/
├── main.go
├── handler/transfer_handler.go
├── service/transfer_service.go
├── repository/transfer_repository.go
├── models/model.go
└── migrations/
    ├── 001_create_wallets.sql
    ├── 002_create_transfers.sql
    ├── 003_create_ledger_entries.sql
    ├── 004_create_idempotency_records.sql
    └── 005_integrity_constraints.sql
```

## Requirements

- Go 1.23+
- PostgreSQL
- Goose migration CLI

Install Goose:

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

## Setup

Create a database:

```sql
CREATE DATABASE wallet_db;
```

Set the connection string:

```bash
export DATABASE_URL="postgres://postgres:your_password@localhost:5432/wallet_db?sslmode=disable"
```

Apply migrations:

```bash
goose -dir migrations postgres "$DATABASE_URL" up
```

Install dependencies and run:

```bash
go mod tidy
go run .
```

The server listens on port 8080 by default. Set `PORT` to change it.

## Seed wallets

Balances are stored in paise (100 paise = ₹1).

```sql
INSERT INTO wallets (id, balance)
VALUES
('WALLET_550e8400-e29b-41d4-a716-446655440001', 100000),
('WALLET_550e8400-e29b-41d4-a716-446655440002', 50000),
('WALLET_550e8400-e29b-41d4-a716-446655440003', 25000);
```

## API

`POST /transfers`

Request:

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "WALLET_550e8400-e29b-41d4-a716-446655440001",
  "toWalletId": "WALLET_550e8400-e29b-41d4-a716-446655440002",
  "amount": 10000
}
```

`amount` is an integer number of paise. The example transfers ₹100.

Example success response:

```json
{
  "transferId": 1,
  "fromWalletId": "WALLET_550e8400-e29b-41d4-a716-446655440001",
  "toWalletId": "WALLET_550e8400-e29b-41d4-a716-446655440002",
  "amount": 10000,
  "status": "PROCESSED",
  "message": "transfer successful"
}
```

Retry the same payload with the same idempotency key to receive the stored original response. Reusing the same key with a different payload returns HTTP 409.

## Concurrency and transaction strategy

- All transfer writes run in one PostgreSQL transaction.
- The idempotency key is claimed using `INSERT ... ON CONFLICT DO NOTHING`.
- A concurrent request using the same key waits on PostgreSQL's unique-key conflict and then reads the committed original response.
- Both wallet rows are locked using `SELECT FOR UPDATE`.
- Wallet rows are locked in sorted ID order to reduce deadlocks.
- Balance updates use SQL arithmetic and a guarded debit.
- Transfer, balance updates, two ledger entries, and stored response commit atomically.
- The database checks that processed transfers have exactly one matching debit and one matching credit.
- Ledger entries cannot be updated or deleted.

## Failure semantics

- Invalid request shape/fields are rejected before the repository is called and are not stored as idempotency outcomes.
- Insufficient balance and missing-wallet outcomes are stored against the idempotency key, so retrying the same key returns the same failure.
- Insufficient-balance attempts are represented by a `FAILED` transfer with no ledger entries.
- Unexpected database errors roll back the transaction, including the idempotency claim. A retry can then safely attempt the operation again.
- The service implements synchronous transfers. A committed `PROCESSING` idempotency row is not expected in this flow.

## Important limitations / production hardening

This is an assignment implementation, not a production banking platform.

- Authentication and wallet ownership checks are not included.
- Add request-size limits, rate limiting, structured logs, metrics, tracing, and graceful shutdown.
- Add PostgreSQL integration tests and run them against a real PostgreSQL instance.
- Consider bounded retry for deadlock/serialization errors.
- Restrict database permissions so application users cannot bypass ledger immutability.
- The ledger trigger validates the two entries but does not independently reconcile stored wallet balances against the complete ledger history.
- Add a separate reversal workflow rather than editing posted ledger entries.
- The API-level exactly-once behavior is scoped to a retained idempotency key and this PostgreSQL database. It is not a claim of exactly-once network delivery.

## PR AI-use disclosure

Describe the actual AI tools used, how they were used, and attach the actual conversation transcript or prompts as required by the assignment. Do not claim that AI-generated code was independently tested unless you have run those tests.


## Integration tests

Use a disposable PostgreSQL test database and apply all migrations to it:

```bash
createdb wallet_test
export TEST_DATABASE_URL="postgres://postgres:your_password@localhost:5432/wallet_test?sslmode=disable"
goose -dir migrations postgres "$TEST_DATABASE_URL" up
go test ./...
```

The integration tests create uniquely named wallets and leave test rows behind. Use a disposable database and recreate it when you want a clean test run.
