package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"coding-challenge/internal/models"

	"github.com/google/uuid"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
)

// Repository handles all database operations.
type Repository struct {
	db *sql.DB
}

// New creates a new repository with the given database connection.
func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// --- Batch Operations ---

// CreateBatch creates a new payout batch and inserts all payouts atomically.
func (r *Repository) CreateBatch(ctx context.Context, items []models.CreatePayoutItem) (*models.PayoutBatch, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	batchID := uuid.New()
	now := time.Now().UTC()
	totalCount := len(items)

	// Insert batch
	_, err = tx.ExecContext(ctx,
		`INSERT INTO payout_batches (id, status, total_count, pending_count, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		batchID, models.BatchStatusPending, totalCount, totalCount, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert batch: %w", err)
	}

	// Insert all payouts
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO payouts (id, batch_id, idempotency_key, vendor_id, vendor_name, amount, currency, bank_account, bank_name, transaction_ids, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`)
	if err != nil {
		return nil, fmt.Errorf("prepare stmt: %w", err)
	}
	defer stmt.Close()

	for _, item := range items {
		payoutID := uuid.New()
		idempotencyKey := fmt.Sprintf("%s:%s", item.VendorID, batchID.String())

		_, err = stmt.ExecContext(ctx,
			payoutID, batchID, idempotencyKey,
			item.VendorID, item.VendorName, item.Amount, item.Currency,
			item.BankAccount, item.BankName, pq.Array(item.TransactionIDs),
			models.PayoutStatusPending, now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("insert payout for vendor %s: %w", item.VendorID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	batch := &models.PayoutBatch{
		ID:         batchID,
		Status:     models.BatchStatusPending,
		TotalCount: totalCount,
		PendingCount: totalCount,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return batch, nil
}

// GetBatch retrieves a batch by ID.
func (r *Repository) GetBatch(ctx context.Context, batchID uuid.UUID) (*models.PayoutBatch, error) {
	batch := &models.PayoutBatch{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, status, total_count, completed_count, failed_count, pending_count,
		        created_at, started_at, completed_at, updated_at
		 FROM payout_batches WHERE id = $1`, batchID,
	).Scan(
		&batch.ID, &batch.Status, &batch.TotalCount, &batch.CompletedCount,
		&batch.FailedCount, &batch.PendingCount, &batch.CreatedAt,
		&batch.StartedAt, &batch.CompletedAt, &batch.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get batch: %w", err)
	}
	return batch, nil
}

// UpdateBatchStatus updates the batch status and timestamps.
func (r *Repository) UpdateBatchStatus(ctx context.Context, batchID uuid.UUID, status string) error {
	now := time.Now().UTC()
	var query string

	switch status {
	case models.BatchStatusInProgress:
		query = `UPDATE payout_batches SET status = $1, started_at = $2, updated_at = $2 WHERE id = $3`
	case models.BatchStatusCompleted, models.BatchStatusPartiallyCompleted, models.BatchStatusFailed:
		query = `UPDATE payout_batches SET status = $1, completed_at = $2, updated_at = $2 WHERE id = $3`
	default:
		query = `UPDATE payout_batches SET status = $1, updated_at = $2 WHERE id = $3`
	}

	_, err := r.db.ExecContext(ctx, query, status, now, batchID)
	return err
}

// RefreshBatchCounts recalculates batch counts from actual payout statuses.
func (r *Repository) RefreshBatchCounts(ctx context.Context, batchID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE payout_batches SET
			completed_count = (SELECT COUNT(*) FROM payouts WHERE batch_id = $1 AND status = 'completed'),
			failed_count    = (SELECT COUNT(*) FROM payouts WHERE batch_id = $1 AND status = 'failed'),
			pending_count   = (SELECT COUNT(*) FROM payouts WHERE batch_id = $1 AND status IN ('pending', 'processing')),
			updated_at      = NOW()
		WHERE id = $1`, batchID)
	return err
}

// --- Payout Operations ---

// GetPendingPayouts retrieves payouts that need processing (pending only).
// Crash recovery for stuck "processing" payouts is handled separately by ResetStuckProcessing.
func (r *Repository) GetPendingPayouts(ctx context.Context, batchID uuid.UUID, limit int) ([]models.Payout, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, batch_id, idempotency_key, vendor_id, vendor_name, amount, currency,
		        bank_account, bank_name, transaction_ids, status, failure_reason, attempt_count, max_retries,
		        created_at, attempted_at, completed_at, updated_at
		 FROM payouts
		 WHERE batch_id = $1 AND status = $2
		 ORDER BY created_at ASC
		 LIMIT $3`,
		batchID, models.PayoutStatusPending, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending payouts: %w", err)
	}
	defer rows.Close()

	return scanPayouts(rows)
}

// ClaimPayout atomically transitions a payout from pending to processing.
// Returns true if the payout was successfully claimed.
// Only claims payouts in "pending" state to prevent concurrent workers from
// double-processing the same payout.
func (r *Repository) ClaimPayout(ctx context.Context, payoutID uuid.UUID) (bool, error) {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx,
		`UPDATE payouts SET status = $1, attempted_at = $2, attempt_count = attempt_count + 1, updated_at = $2
		 WHERE id = $3 AND status = $4`,
		models.PayoutStatusProcessing, now, payoutID,
		models.PayoutStatusPending,
	)
	if err != nil {
		return false, fmt.Errorf("claim payout: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected > 0, nil
}

// CompletePayout marks a payout as completed.
func (r *Repository) CompletePayout(ctx context.Context, payoutID uuid.UUID) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE payouts SET status = $1, completed_at = $2, updated_at = $2 WHERE id = $3`,
		models.PayoutStatusCompleted, now, payoutID,
	)
	return err
}

// FailPayout marks a payout as failed with a reason.
func (r *Repository) FailPayout(ctx context.Context, payoutID uuid.UUID, reason string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE payouts SET status = $1, failure_reason = $2, updated_at = $3 WHERE id = $4`,
		models.PayoutStatusFailed, reason, now, payoutID,
	)
	return err
}

// RequeuePayout puts a failed retryable payout back to pending.
func (r *Repository) RequeuePayout(ctx context.Context, payoutID uuid.UUID) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE payouts SET status = $1, failure_reason = NULL, updated_at = $2
		 WHERE id = $3 AND attempt_count < max_retries`,
		models.PayoutStatusPending, now, payoutID,
	)
	return err
}

// GetPayoutsByBatch retrieves payouts for a batch with optional status filter and pagination.
func (r *Repository) GetPayoutsByBatch(ctx context.Context, batchID uuid.UUID, status string, page, pageSize int) ([]models.Payout, int, error) {
	offset := (page - 1) * pageSize

	// Count total
	var countQuery string
	var totalCount int
	if status != "" {
		countQuery = `SELECT COUNT(*) FROM payouts WHERE batch_id = $1 AND status = $2`
		err := r.db.QueryRowContext(ctx, countQuery, batchID, status).Scan(&totalCount)
		if err != nil {
			return nil, 0, err
		}
	} else {
		countQuery = `SELECT COUNT(*) FROM payouts WHERE batch_id = $1`
		err := r.db.QueryRowContext(ctx, countQuery, batchID).Scan(&totalCount)
		if err != nil {
			return nil, 0, err
		}
	}

	// Fetch page
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, batch_id, idempotency_key, vendor_id, vendor_name, amount, currency,
			        bank_account, bank_name, transaction_ids, status, failure_reason, attempt_count, max_retries,
			        created_at, attempted_at, completed_at, updated_at
			 FROM payouts WHERE batch_id = $1 AND status = $2
			 ORDER BY created_at ASC LIMIT $3 OFFSET $4`,
			batchID, status, pageSize, offset)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, batch_id, idempotency_key, vendor_id, vendor_name, amount, currency,
			        bank_account, bank_name, transaction_ids, status, failure_reason, attempt_count, max_retries,
			        created_at, attempted_at, completed_at, updated_at
			 FROM payouts WHERE batch_id = $1
			 ORDER BY created_at ASC LIMIT $2 OFFSET $3`,
			batchID, pageSize, offset)
	}
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	payouts, err := scanPayouts(rows)
	return payouts, totalCount, err
}

// GetBatchStatistics returns detailed statistics for a batch.
func (r *Repository) GetBatchStatistics(ctx context.Context, batchID uuid.UUID) (*models.BatchStatistics, error) {
	stats := &models.BatchStatistics{}
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE status = 'completed') as completed,
			COUNT(*) FILTER (WHERE status = 'failed') as failed,
			COUNT(*) FILTER (WHERE status = 'pending') as pending,
			COUNT(*) FILTER (WHERE status = 'processing') as processing
		FROM payouts WHERE batch_id = $1`, batchID,
	).Scan(&stats.Total, &stats.Completed, &stats.Failed, &stats.Pending, &stats.Processing)
	if err != nil {
		return nil, err
	}

	if stats.Total > 0 {
		stats.SuccessRate = float64(stats.Completed) / float64(stats.Total) * 100
		processed := stats.Completed + stats.Failed
		stats.CompletionRate = float64(processed) / float64(stats.Total) * 100
	}
	return stats, nil
}

// ResetStuckProcessing resets payouts stuck in "processing" back to "pending" (for crash recovery).
func (r *Repository) ResetStuckProcessing(ctx context.Context, batchID uuid.UUID) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE payouts SET status = $1, updated_at = NOW()
		 WHERE batch_id = $2 AND status = $3 AND attempt_count < max_retries`,
		models.PayoutStatusPending, batchID, models.PayoutStatusProcessing,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// RetryFailedPayouts resets retryable failed payouts back to pending.
func (r *Repository) RetryFailedPayouts(ctx context.Context, batchID uuid.UUID) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`UPDATE payouts SET status = $1, failure_reason = NULL, updated_at = NOW()
		 WHERE batch_id = $2 AND status = $3 AND attempt_count < max_retries
		 AND failure_reason IN ($4, $5, $6)`,
		models.PayoutStatusPending, batchID, models.PayoutStatusFailed,
		models.FailureBankTimeout, models.FailureRateLimited, models.FailureInsufficientFunds,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// --- Attempt Logging ---

// LogAttempt records a payout attempt for audit.
func (r *Repository) LogAttempt(ctx context.Context, attempt *models.PayoutAttempt) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO payout_attempts (id, payout_id, attempt_num, status, error, started_at, finished_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		attempt.ID, attempt.PayoutID, attempt.AttemptNum, attempt.Status, attempt.Error,
		attempt.StartedAt, attempt.FinishedAt,
	)
	return err
}

// --- Helpers ---

func scanPayouts(rows *sql.Rows) ([]models.Payout, error) {
	var payouts []models.Payout
	for rows.Next() {
		var p models.Payout
		err := rows.Scan(
			&p.ID, &p.BatchID, &p.IdempotencyKey, &p.VendorID, &p.VendorName,
			&p.Amount, &p.Currency, &p.BankAccount, &p.BankName,
			pq.Array(&p.TransactionIDs), &p.Status,
			&p.FailureReason, &p.AttemptCount, &p.MaxRetries,
			&p.CreatedAt, &p.AttemptedAt, &p.CompletedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan payout: %w", err)
		}
		payouts = append(payouts, p)
	}
	return payouts, rows.Err()
}
