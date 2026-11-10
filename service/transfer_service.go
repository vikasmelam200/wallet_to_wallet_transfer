package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"wallet-service/models"
)

type TransferRepository interface {
	ExecuteTransfer(ctx context.Context, req models.TransferRequest, requestHash string) (models.TransferResult, error)
}

type TransferService struct {
	repo TransferRepository
}

func NewTransferService(repo TransferRepository) *TransferService {
	return &TransferService{repo: repo}
}

func (s *TransferService) CreateTransfer(ctx context.Context, req models.TransferRequest) (models.TransferResult, error) {
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.FromWalletID = strings.TrimSpace(req.FromWalletID)
	req.ToWalletID = strings.TrimSpace(req.ToWalletID)

	// idempotency behavior and business logic
	if req.IdempotencyKey == "" {
		return models.TransferResult{}, fmt.Errorf("%w: idempotencyKey is required", models.ErrInvalidRequest)
	}
	if len(req.IdempotencyKey) > 255 {
		return models.TransferResult{}, fmt.Errorf("%w: idempotencyKey must be at most 255 characters", models.ErrInvalidRequest)
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return models.TransferResult{}, fmt.Errorf("%w: fromWalletId and toWalletId are required", models.ErrInvalidRequest)
	}
	if req.FromWalletID == req.ToWalletID {
		return models.TransferResult{}, fmt.Errorf("%w: source and destination wallets must be different", models.ErrInvalidRequest)
	}
	if req.Amount <= 0 {
		return models.TransferResult{}, fmt.Errorf("%w: amount must be greater than zero", models.ErrInvalidRequest)
	}

	// Hash only the business payload. The idempotency key itself is not part
	// of the hash, so reusing a key with a changed payload is detectable.
	canonicalPayload := struct {
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}{
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
	}
	raw, err := json.Marshal(canonicalPayload)
	if err != nil {
		return models.TransferResult{}, err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])

	return s.repo.ExecuteTransfer(ctx, req, hash)
}
