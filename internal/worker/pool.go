package worker

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"coding-challenge/internal/models"
	"coding-challenge/internal/repository"
	"coding-challenge/internal/service"

	"github.com/google/uuid"
)

// Pool manages concurrent payout processing workers.
type Pool struct {
	repo        *repository.Repository
	concurrency int
	chunkSize   int
	stopCh      chan struct{}
	running     atomic.Bool
}

// NewPool creates a new worker pool.
func NewPool(repo *repository.Repository, concurrency, chunkSize int) *Pool {
	return &Pool{
		repo:        repo,
		concurrency: concurrency,
		chunkSize:   chunkSize,
		stopCh:      make(chan struct{}),
	}
}

// ProcessBatch processes all pending payouts in a batch using a worker pool.
// It is resumable — only processes pending/stuck payouts.
func (p *Pool) ProcessBatch(ctx context.Context, batchID uuid.UUID) error {
	if !p.running.CompareAndSwap(false, true) {
		return nil // Already running
	}
	defer p.running.Store(false)

	log.Printf("[processor] Starting batch %s with concurrency=%d, chunk=%d", batchID, p.concurrency, p.chunkSize)

	// Step 1: Reset any payouts stuck in "processing" from a previous crash
	reset, err := p.repo.ResetStuckProcessing(ctx, batchID)
	if err != nil {
		return err
	}
	if reset > 0 {
		log.Printf("[processor] Reset %d stuck payouts back to pending", reset)
	}

	// Step 2: Mark batch as in_progress
	if err := p.repo.UpdateBatchStatus(ctx, batchID, models.BatchStatusInProgress); err != nil {
		return err
	}

	// Step 3: Process in chunks
	for {
		select {
		case <-p.stopCh:
			log.Printf("[processor] Received stop signal, pausing batch %s", batchID)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Fetch next chunk of pending payouts
		payouts, err := p.repo.GetPendingPayouts(ctx, batchID, p.chunkSize)
		if err != nil {
			return err
		}

		if len(payouts) == 0 {
			break // All done
		}

		log.Printf("[processor] Processing chunk of %d payouts", len(payouts))

		// Process chunk with worker pool
		p.processChunk(ctx, payouts)

		// Refresh batch counts
		if err := p.repo.RefreshBatchCounts(ctx, batchID); err != nil {
			log.Printf("[processor] Warning: failed to refresh counts: %v", err)
		}
	}

	// Step 4: Determine final batch status
	stats, err := p.repo.GetBatchStatistics(ctx, batchID)
	if err != nil {
		return err
	}

	var finalStatus string
	switch {
	case stats.Failed == 0:
		finalStatus = models.BatchStatusCompleted
	case stats.Completed == 0:
		finalStatus = models.BatchStatusFailed
	default:
		finalStatus = models.BatchStatusPartiallyCompleted
	}

	if err := p.repo.UpdateBatchStatus(ctx, batchID, finalStatus); err != nil {
		return err
	}

	// Final count refresh
	_ = p.repo.RefreshBatchCounts(ctx, batchID)

	log.Printf("[processor] Batch %s finished: %s (completed=%d, failed=%d)",
		batchID, finalStatus, stats.Completed, stats.Failed)

	return nil
}

// processChunk processes a slice of payouts concurrently.
func (p *Pool) processChunk(ctx context.Context, payouts []models.Payout) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, p.concurrency)

	for _, payout := range payouts {
		select {
		case <-p.stopCh:
			break
		case <-ctx.Done():
			break
		default:
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire slot

		go func(po models.Payout) {
			defer wg.Done()
			defer func() { <-sem }() // Release slot

			p.processSinglePayout(ctx, po)
		}(payout)
	}

	wg.Wait()
}

// processSinglePayout handles one payout with claim → execute → record.
func (p *Pool) processSinglePayout(ctx context.Context, payout models.Payout) {
	// Step 1: Claim the payout (atomic transition to "processing")
	claimed, err := p.repo.ClaimPayout(ctx, payout.ID)
	if err != nil {
		log.Printf("[worker] Error claiming payout %s: %v", payout.ID, err)
		return
	}
	if !claimed {
		return // Already being processed by another worker
	}

	attemptStart := time.Now().UTC()

	// Step 2: Simulate the bank transfer
	result := service.SimulateBankTransfer(payout.VendorID, payout.Amount)

	attemptEnd := time.Now().UTC()

	// Step 3: Record the attempt
	attempt := &models.PayoutAttempt{
		ID:         uuid.New(),
		PayoutID:   payout.ID,
		AttemptNum: payout.AttemptCount + 1,
		StartedAt:  attemptStart,
		FinishedAt: &attemptEnd,
	}

	if result.Success {
		attempt.Status = models.PayoutStatusCompleted
		if err := p.repo.CompletePayout(ctx, payout.ID); err != nil {
			log.Printf("[worker] Error completing payout %s: %v", payout.ID, err)
		}
	} else {
		attempt.Status = models.PayoutStatusFailed
		attempt.Error = &result.FailureCode

		if result.IsRetryable && payout.AttemptCount+1 < payout.MaxRetries {
			// Retryable: put back to pending
			if err := p.repo.RequeuePayout(ctx, payout.ID); err != nil {
				log.Printf("[worker] Error requeuing payout %s: %v", payout.ID, err)
			}
		} else {
			// Permanent failure or max retries exceeded
			if err := p.repo.FailPayout(ctx, payout.ID, result.FailureCode); err != nil {
				log.Printf("[worker] Error failing payout %s: %v", payout.ID, err)
			}
		}
	}

	// Log the attempt
	if err := p.repo.LogAttempt(ctx, attempt); err != nil {
		log.Printf("[worker] Error logging attempt for payout %s: %v", payout.ID, err)
	}
}

// Stop signals the pool to stop processing after the current chunk.
func (p *Pool) Stop() {
	if p.running.Load() {
		close(p.stopCh)
	}
}

// IsRunning returns whether the pool is currently processing.
func (p *Pool) IsRunning() bool {
	return p.running.Load()
}
