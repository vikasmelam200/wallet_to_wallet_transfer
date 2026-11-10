package tests

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"wallet-service/models"
	"wallet-service/repository"
	"wallet-service/service"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func testService(t *testing.T) (*gorm.DB, *service.TransferService) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to a disposable PostgreSQL database with migrations applied")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}

	repo := repository.NewTransferRepository(db)
	return db, service.NewTransferService(repo)
}

func newWalletID() string {
	return "WALLET_" + uuid.NewString()
}

func insertWallet(t *testing.T, db *gorm.DB, balance int64) string {
	t.Helper()
	id := newWalletID()
	if err := db.Create(&models.Wallet{ID: id, Balance: balance}).Error; err != nil {
		t.Fatalf("insert wallet: %v", err)
	}
	return id
}

func TestTransferAndIdempotentReplay(t *testing.T) {
	db, svc := testService(t)
	from := insertWallet(t, db, 10000)
	to := insertWallet(t, db, 5000)

	req := models.TransferRequest{
		IdempotencyKey: "test-" + uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         2500,
	}

	first, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	if first.HTTPStatus != 200 || first.Body.Status != models.TransferProcessed {
		t.Fatalf("unexpected first result: %+v", first)
	}

	replay, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replay.Replayed {
		t.Fatal("expected replay flag")
	}
	if replay.Body.TransferID == nil || first.Body.TransferID == nil ||
		*replay.Body.TransferID != *first.Body.TransferID {
		t.Fatalf("replay did not return original transfer: first=%+v replay=%+v", first, replay)
	}

	var fromWallet, toWallet models.Wallet
	if err := db.First(&fromWallet, "id = ?", from).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&toWallet, "id = ?", to).Error; err != nil {
		t.Fatal(err)
	}
	if fromWallet.Balance != 7500 || toWallet.Balance != 7500 {
		t.Fatalf("unexpected balances: from=%d to=%d", fromWallet.Balance, toWallet.Balance)
	}

	var entries int64
	if err := db.Model(&models.LedgerEntry{}).
		Where("transfer_id = ?", *first.Body.TransferID).
		Count(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if entries != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", entries)
	}
}

func TestSameIdempotencyKeyDifferentPayloadConflicts(t *testing.T) {
	db, svc := testService(t)
	from := insertWallet(t, db, 10000)
	to := insertWallet(t, db, 10000)

	key := "test-" + uuid.NewString()
	first := models.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         1000,
	}
	if _, err := svc.CreateTransfer(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	first.Amount = 2000
	_, err := svc.CreateTransfer(context.Background(), first)
	if err != models.ErrIdempotencyConflict {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestInsufficientBalanceIsStableOnRetry(t *testing.T) {
	db, svc := testService(t)
	from := insertWallet(t, db, 100)
	to := insertWallet(t, db, 100)

	req := models.TransferRequest{
		IdempotencyKey: "test-" + uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         500,
	}

	first, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.HTTPStatus != 422 || first.Body.Status != models.TransferFailed {
		t.Fatalf("expected stored failure, got %+v", first)
	}

	// Add funds after the first attempt. Replaying the same key must still
	// return the original insufficient-balance result.
	if err := db.Model(&models.Wallet{}).Where("id = ?", from).
		Update("balance", 1000).Error; err != nil {
		t.Fatal(err)
	}

	replay, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.HTTPStatus != 422 {
		t.Fatalf("expected original failure replay, got %+v", replay)
	}
}

func TestConcurrentDebitsDoNotOverspend(t *testing.T) {
	db, svc := testService(t)
	from := insertWallet(t, db, 1000)
	toA := insertWallet(t, db, 0)
	toB := insertWallet(t, db, 0)

	requests := []models.TransferRequest{
		{
			IdempotencyKey: "test-" + uuid.NewString(),
			FromWalletID:   from,
			ToWalletID:     toA,
			Amount:         800,
		},
		{
			IdempotencyKey: "test-" + uuid.NewString(),
			FromWalletID:   from,
			ToWalletID:     toB,
			Amount:         700,
		},
	}

	var successCount int32
	var failureCount int32
	var wg sync.WaitGroup
	errs := make(chan error, len(requests))

	for _, req := range requests {
		req := req
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := svc.CreateTransfer(context.Background(), req)
			if err != nil {
				errs <- err
				return
			}
			switch result.HTTPStatus {
			case 200:
				atomic.AddInt32(&successCount, 1)
			case 422:
				atomic.AddInt32(&failureCount, 1)
			default:
				errs <- fmt.Errorf("unexpected HTTP status: %d", result.HTTPStatus)
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if successCount != 1 || failureCount != 1 {
		t.Fatalf("expected one success and one insufficient-balance failure; success=%d failure=%d",
			successCount, failureCount)
	}

	var wallet models.Wallet
	if err := db.First(&wallet, "id = ?", from).Error; err != nil {
		t.Fatal(err)
	}
	if wallet.Balance != 200 && wallet.Balance != 300 {
		t.Fatalf("unexpected final sender balance: %d", wallet.Balance)
	}
}
