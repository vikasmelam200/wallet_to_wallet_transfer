package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"

	"wallet-service/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TransferRepository struct {
	db *gorm.DB
}

func NewTransferRepository(db *gorm.DB) *TransferRepository {
	return &TransferRepository{db: db}
}

func (r *TransferRepository) ExecuteTransfer(ctx context.Context, req models.TransferRequest, requestHash string, ) (models.TransferResult, error) {
	var result models.TransferResult

	
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Claim the idempotency key. A concurrent INSERT for the same unique
		// key waits for the first transaction to commit or roll back.
		record := models.IdempotencyRecord{
			IdempotencyKey: req.IdempotencyKey,
			RequestHash:    requestHash,
			Status:         models.IdempotencyProcessing,
		}
		// 
		insert := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "idempotency_key"}},
			DoNothing: true,
		}).Create(&record)
		
		// transaction will rollback if any error is returned from this function, including the error returned by the insert operation.
		if insert.Error != nil {
			return insert.Error
		}

		if insert.RowsAffected == 0 {
			var existing models.IdempotencyRecord
			if err := tx.Where("idempotency_key = ?", req.IdempotencyKey).Take(&existing).Error; err != nil {
				return err
			}
			if existing.RequestHash != requestHash {
				return models.ErrIdempotencyConflict
			}
			if existing.Status == models.IdempotencyProcessing {
				return models.ErrIdempotencyPending
			}
			if existing.ResponseCode == nil || len(existing.ResponseBody) == 0 {
				return errors.New("stored idempotency response is incomplete")
			}
			if err := json.Unmarshal(existing.ResponseBody, &result.Body); err != nil {
				return err
			}
			result.HTTPStatus = *existing.ResponseCode
			result.Replayed = true
			return nil
		}

		// Lock wallets in deterministic order to reduce deadlocks when
		// opposite-direction transfers execute concurrently.
		walletIDs := []string{req.FromWalletID, req.ToWalletID}
		sort.Strings(walletIDs)

		wallets := make(map[string]models.Wallet, 2)
		for _, walletID := range walletIDs {
			var wallet models.Wallet
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?", walletID).
				Take(&wallet).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return r.persistFailure(tx, &record, nil, 404, models.TransferResponse{
						FromWalletID: req.FromWalletID,
						ToWalletID:   req.ToWalletID,
						Amount:       req.Amount,
						Status:       models.TransferFailed,
						Message:      "wallet not found",
					}, models.IdempotencyFailed, &result)
				}
				return err
			}
			wallets[walletID] = wallet
		}

		fromWallet := wallets[req.FromWalletID]
		toWallet := wallets[req.ToWalletID]

		if fromWallet.Balance < req.Amount {
			transfer := models.Transfer{
				FromWalletID: req.FromWalletID,
				ToWalletID:   req.ToWalletID,
				Amount:       req.Amount,
				Status:       models.TransferFailed,
			}
			if err := tx.Create(&transfer).Error; err != nil {
				return err
			}
			return r.persistFailure(tx, &record, &transfer.ID, 422, models.TransferResponse{
				TransferID:   &transfer.ID,
				FromWalletID: req.FromWalletID,
				ToWalletID:   req.ToWalletID,
				Amount:       req.Amount,
				Status:       models.TransferFailed,
				Message:      "insufficient balance",
			}, models.IdempotencyFailed, &result)
		}

		if toWallet.Balance > math.MaxInt64-req.Amount {
			transfer := models.Transfer{
				FromWalletID: req.FromWalletID,
				ToWalletID:   req.ToWalletID,
				Amount:       req.Amount,
				Status:       models.TransferFailed,
			}
			if err := tx.Create(&transfer).Error; err != nil {
				return err
			}
			return r.persistFailure(tx, &record, &transfer.ID, 422, models.TransferResponse{
				TransferID:   &transfer.ID,
				FromWalletID: req.FromWalletID,
				ToWalletID:   req.ToWalletID,
				Amount:       req.Amount,
				Status:       models.TransferFailed,
				Message:      "destination wallet balance would overflow",
			}, models.IdempotencyFailed, &result)
		}

		transfer := models.Transfer{
			FromWalletID: req.FromWalletID,
			ToWalletID:   req.ToWalletID,
			Amount:       req.Amount,
			Status:       models.TransferPending,
		}
		if err := tx.Create(&transfer).Error; err != nil {
			return err
		}

		// Guarded updates are an additional defense even though the rows
		// are already locked.
		debit := tx.Model(&models.Wallet{}).
			Where("id = ? AND balance >= ?", req.FromWalletID, req.Amount).
			Update("balance", gorm.Expr("balance - ?", req.Amount))
		if debit.Error != nil {
			return debit.Error
		}
		if debit.RowsAffected != 1 {
			return errors.New("source balance changed unexpectedly")
		}

		credit := tx.Model(&models.Wallet{}).
			Where("id = ?", req.ToWalletID).
			Update("balance", gorm.Expr("balance + ?", req.Amount))
		if credit.Error != nil {
			return credit.Error
		}
		if credit.RowsAffected != 1 {
			return errors.New("destination wallet disappeared")
		}

		entries := []models.LedgerEntry{
			{
				WalletID:   req.FromWalletID,
				TransferID: transfer.ID,
				Type:       models.LedgerDebit,
				Amount:     req.Amount,
			},
			{
				WalletID:   req.ToWalletID,
				TransferID: transfer.ID,
				Type:       models.LedgerCredit,
				Amount:     req.Amount,
			},
		}
		if err := tx.Create(&entries).Error; err != nil {
			return err
		}

		// The deferred PostgreSQL constraint trigger checks the final state
		// at commit: exactly one matching debit and credit.
		updated := tx.Model(&models.Transfer{}).
			Where("id = ? AND status = ?", transfer.ID, models.TransferPending).
			Update("status", models.TransferProcessed)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errors.New("invalid transfer state transition")
		}

		response := models.TransferResponse{
			TransferID:   &transfer.ID,
			FromWalletID: req.FromWalletID,
			ToWalletID:   req.ToWalletID,
			Amount:       req.Amount,
			Status:       models.TransferProcessed,
			Message:      "transfer successful",
		}
		body, err := json.Marshal(response)
		if err != nil {
			return err
		}
		code := 200
		if err := tx.Model(&models.IdempotencyRecord{}).
			Where("id = ?", record.ID).
			Updates(map[string]interface{}{
				"transfer_id":   transfer.ID,
				"status":        models.IdempotencyCompleted,
				"response_body": string(body),
				"response_code": code,
			}).Error; err != nil {
			return err
		}

		result = models.TransferResult{HTTPStatus: code, Body: response}
		return nil
	})

	if err != nil {
		return models.TransferResult{}, err
	}
	return result, nil
}

func (r *TransferRepository) persistFailure(
	tx *gorm.DB,
	record *models.IdempotencyRecord,
	transferID *int64,
	httpCode int,
	response models.TransferResponse,
	status models.IdempotencyStatus,
	result *models.TransferResult,
) error {
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}

	updates := map[string]interface{}{
		"status":        status,
		"response_body": string(body),
		"response_code": httpCode,
	}
	if transferID != nil {
		updates["transfer_id"] = *transferID
	}
	if err := tx.Model(&models.IdempotencyRecord{}).
		Where("id = ?", record.ID).
		Updates(updates).Error; err != nil {
		return err
	}

	*result = models.TransferResult{
		HTTPStatus: httpCode,
		Body:       response,
	}
	return nil
}
