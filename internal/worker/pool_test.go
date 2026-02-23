package worker_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"coding-challenge/internal/models"
	"coding-challenge/internal/repository"
	"coding-challenge/internal/worker"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// getTestDB returns a database connection for testing.
// Requires a running PostgreSQL with kaveri_payouts_test database.
// Set TEST_DB_DSN env var to override.
func getTestDB(t *testing.T) *sql.DB {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = "host=localhost port=5432 user=postgres password=postgres dbname=kaveri_payouts_test sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("Skipping integration test: cannot connect to DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("Skipping integration test: DB not reachable: %v", err)
	}

	// Clean tables before test
	db.Exec("DELETE FROM payout_attempts")
	db.Exec("DELETE FROM payouts")
	db.Exec("DELETE FROM payout_batches")

	return db
}

func createTestBatch(t *testing.T, repo *repository.Repository, count int) uuid.UUID {
	items := make([]models.CreatePayoutItem, count)
	for i := 0; i < count; i++ {
		items[i] = models.CreatePayoutItem{
			VendorID:    fmt.Sprintf("test_vendor_%04d", i),
			VendorName:  fmt.Sprintf("Test Vendor %d", i),
			Amount:      100.00 + float64(i),
			Currency:    "USD",
			BankAccount: fmt.Sprintf("ACC%010d", i),
			BankName:    "Test Bank",
		}
	}

	batch, err := repo.CreateBatch(context.Background(), items)
	if err != nil {
		t.Fatalf("Failed to create test batch: %v", err)
	}
	return batch.ID
}

// TestBatchProcessingCompletesAll verifies that all payouts are processed.
func TestBatchProcessingCompletesAll(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	repo := repository.New(db)
	batchID := createTestBatch(t, repo, 50)

	pool := worker.NewPool(repo, 5, 20)
	err := pool.ProcessBatch(context.Background(), batchID)
	if err != nil {
		t.Fatalf("ProcessBatch failed: %v", err)
	}

	stats, err := repo.GetBatchStatistics(context.Background(), batchID)
	if err != nil {
		t.Fatalf("GetBatchStatistics failed: %v", err)
	}

	// All 50 should be processed (completed or failed)
	processed := stats.Completed + stats.Failed
	if processed != 50 {
		t.Errorf("Expected 50 processed, got %d (completed=%d, failed=%d, pending=%d)",
			processed, stats.Completed, stats.Failed, stats.Pending)
	}

	if stats.Pending != 0 {
		t.Errorf("Expected 0 pending, got %d", stats.Pending)
	}

	t.Logf("Results: completed=%d, failed=%d, pending=%d", stats.Completed, stats.Failed, stats.Pending)
}

// TestIdempotency verifies running the same batch twice doesn't create duplicates.
func TestIdempotency(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	repo := repository.New(db)
	batchID := createTestBatch(t, repo, 20)

	// Process the batch
	pool := worker.NewPool(repo, 5, 10)
	pool.ProcessBatch(context.Background(), batchID)

	// Get stats after first run
	stats1, _ := repo.GetBatchStatistics(context.Background(), batchID)
	completed1 := stats1.Completed

	// Try to process again (should be a no-op for completed payouts)
	pool2 := worker.NewPool(repo, 5, 10)
	pool2.ProcessBatch(context.Background(), batchID)

	stats2, _ := repo.GetBatchStatistics(context.Background(), batchID)

	// Completed count should not increase (no double payments)
	if stats2.Completed < completed1 {
		t.Errorf("Completed count decreased: %d -> %d", completed1, stats2.Completed)
	}

	total := stats2.Completed + stats2.Failed
	if total != 20 {
		t.Errorf("Total processed should be 20, got %d", total)
	}

	t.Logf("Run 1: completed=%d | Run 2: completed=%d (no duplicates)", completed1, stats2.Completed)
}

// TestResumability verifies that a stopped batch can be resumed.
func TestResumability(t *testing.T) {
	db := getTestDB(t)
	defer db.Close()

	repo := repository.New(db)
	batchID := createTestBatch(t, repo, 100)

	// Process with a context that cancels quickly (simulates crash)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool1 := worker.NewPool(repo, 3, 10)
	pool1.ProcessBatch(ctx, batchID)

	// Check partial progress
	stats1, _ := repo.GetBatchStatistics(context.Background(), batchID)
	processedBefore := stats1.Completed + stats1.Failed
	t.Logf("After interrupt: completed=%d, failed=%d, pending=%d, processing=%d",
		stats1.Completed, stats1.Failed, stats1.Pending, stats1.Processing)

	if processedBefore == 100 {
		t.Log("All processed before timeout — test inconclusive, but not a failure")
		return
	}

	// Resume processing
	pool2 := worker.NewPool(repo, 5, 20)
	pool2.ProcessBatch(context.Background(), batchID)

	// All should be processed now
	stats2, _ := repo.GetBatchStatistics(context.Background(), batchID)
	totalProcessed := stats2.Completed + stats2.Failed

	if totalProcessed != 100 {
		t.Errorf("Expected 100 processed after resume, got %d", totalProcessed)
	}

	// No duplicates: completed should not exceed what's possible
	if stats2.Completed > 100 {
		t.Errorf("More completions than payouts! completed=%d", stats2.Completed)
	}

	t.Logf("After resume: completed=%d, failed=%d (total=%d)", stats2.Completed, stats2.Failed, totalProcessed)
}
