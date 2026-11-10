package models

import (
	"errors"
	"time"
)

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

type LedgerType string

const (
	LedgerDebit  LedgerType = "DEBIT"
	LedgerCredit LedgerType = "CREDIT"
)

type IdempotencyStatus string

const (
	IdempotencyProcessing IdempotencyStatus = "PROCESSING"
	IdempotencyCompleted  IdempotencyStatus = "COMPLETED"
	IdempotencyFailed     IdempotencyStatus = "FAILED"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")
	ErrIdempotencyPending  = errors.New("request with this idempotency key is still processing")
	ErrInvalidRequest      = errors.New("invalid transfer request")
)

type Wallet struct {
	ID        string    `json:"id" gorm:"column:id;primaryKey"`
	Balance   int64     `json:"balance" gorm:"column:balance;not null"`
	CreatedAt time.Time `json:"createdAt" gorm:"column:created_at"`
}

func (Wallet) TableName() string { return "wallets" }

type Transfer struct {
	ID           int64          `json:"id" gorm:"column:id;primaryKey"`
	FromWalletID string         `json:"fromWalletId" gorm:"column:from_wallet_id"`
	ToWalletID   string         `json:"toWalletId" gorm:"column:to_wallet_id"`
	Amount       int64          `json:"amount" gorm:"column:amount"`
	Status       TransferStatus `json:"status" gorm:"column:status"`
	CreatedAt    time.Time      `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt    time.Time      `json:"updatedAt" gorm:"column:updated_at"`
}

func (Transfer) TableName() string { return "transfers" }

type LedgerEntry struct {
	ID         int64      `json:"id" gorm:"column:id;primaryKey"`
	WalletID   string     `json:"walletId" gorm:"column:wallet_id"`
	TransferID int64      `json:"transferId" gorm:"column:transfer_id"`
	Type       LedgerType `json:"type" gorm:"column:type"`
	Amount     int64      `json:"amount" gorm:"column:amount"`
	CreatedAt  time.Time  `json:"createdAt" gorm:"column:created_at"`
}

func (LedgerEntry) TableName() string { return "ledger_entries" }

type IdempotencyRecord struct {
	ID             int64             `json:"id" gorm:"column:id;primaryKey"`
	IdempotencyKey string            `json:"idempotencyKey" gorm:"column:idempotency_key"`
	RequestHash    string            `json:"requestHash" gorm:"column:request_hash"`
	TransferID     *int64            `json:"transferId,omitempty" gorm:"column:transfer_id"`
	Status         IdempotencyStatus `json:"status" gorm:"column:status"`
	ResponseBody   []byte            `json:"-" gorm:"column:response_body;type:jsonb"`
	ResponseCode   *int              `json:"responseCode,omitempty" gorm:"column:response_code"`
	CreatedAt      time.Time         `json:"createdAt" gorm:"column:created_at"`
	UpdatedAt      time.Time         `json:"updatedAt" gorm:"column:updated_at"`
}

func (IdempotencyRecord) TableName() string { return "idempotency_records" }

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type TransferResponse struct {
	TransferID   *int64         `json:"transferId,omitempty"`
	FromWalletID string         `json:"fromWalletId"`
	ToWalletID   string         `json:"toWalletId"`
	Amount       int64          `json:"amount"`
	Status       TransferStatus `json:"status"`
	Message      string         `json:"message"`
}

type TransferResult struct {
	HTTPStatus int
	Body       TransferResponse
	Replayed   bool
}
