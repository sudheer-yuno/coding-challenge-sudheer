package models

import (
	"time"

	"github.com/google/uuid"
)

// Batch statuses
const (
	BatchStatusPending            = "pending"
	BatchStatusInProgress         = "in_progress"
	BatchStatusCompleted          = "completed"
	BatchStatusFailed             = "failed"
	BatchStatusPartiallyCompleted = "partially_completed"
)

// Payout statuses
const (
	PayoutStatusPending    = "pending"
	PayoutStatusProcessing = "processing"
	PayoutStatusCompleted  = "completed"
	PayoutStatusFailed     = "failed"
)

// Failure reasons (simulated)
const (
	FailureInvalidBankAccount = "INVALID_BANK_ACCOUNT"
	FailureInsufficientFunds  = "INSUFFICIENT_FUNDS"
	FailureBankTimeout        = "BANK_API_TIMEOUT"
	FailureAccountBlocked     = "ACCOUNT_BLOCKED"
	FailureRateLimited        = "RATE_LIMITED"
)

// PayoutBatch represents a batch of payouts to be processed.
type PayoutBatch struct {
	ID             uuid.UUID  `json:"id"`
	Status         string     `json:"status"`
	TotalCount     int        `json:"total_count"`
	CompletedCount int        `json:"completed_count"`
	FailedCount    int        `json:"failed_count"`
	PendingCount   int        `json:"pending_count"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Payout represents an individual payout within a batch.
type Payout struct {
	ID             uuid.UUID  `json:"id"`
	BatchID        uuid.UUID  `json:"batch_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	VendorID       string     `json:"vendor_id"`
	VendorName     string     `json:"vendor_name,omitempty"`
	Amount         float64    `json:"amount"`
	Currency       string     `json:"currency"`
	BankAccount    string     `json:"bank_account,omitempty"`
	BankName       string     `json:"bank_name,omitempty"`
	Status         string     `json:"status"`
	FailureReason  *string    `json:"failure_reason,omitempty"`
	AttemptCount   int        `json:"attempt_count"`
	MaxRetries     int        `json:"max_retries"`
	CreatedAt      time.Time  `json:"created_at"`
	AttemptedAt    *time.Time `json:"attempted_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// PayoutAttempt records each attempt to process a payout.
type PayoutAttempt struct {
	ID         uuid.UUID  `json:"id"`
	PayoutID   uuid.UUID  `json:"payout_id"`
	AttemptNum int        `json:"attempt_num"`
	Status     string     `json:"status"`
	Error      *string    `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// --- API Request/Response types ---

// CreateBatchRequest is the payload for creating a new batch.
type CreateBatchRequest struct {
	Payouts []CreatePayoutItem `json:"payouts" binding:"required,min=1"`
}

// CreatePayoutItem represents a single payout in a batch creation request.
type CreatePayoutItem struct {
	VendorID    string  `json:"vendor_id" binding:"required"`
	VendorName  string  `json:"vendor_name"`
	Amount      float64 `json:"amount" binding:"required,gt=0"`
	Currency    string  `json:"currency" binding:"required"`
	BankAccount string  `json:"bank_account" binding:"required"`
	BankName    string  `json:"bank_name"`
}

// BatchSummary is the response for batch status queries.
type BatchSummary struct {
	Batch      PayoutBatch       `json:"batch"`
	Statistics BatchStatistics   `json:"statistics"`
}

// BatchStatistics holds aggregated counts.
type BatchStatistics struct {
	Total          int     `json:"total"`
	Completed      int     `json:"completed"`
	Failed         int     `json:"failed"`
	Pending        int     `json:"pending"`
	Processing     int     `json:"processing"`
	SuccessRate    float64 `json:"success_rate_percent"`
	CompletionRate float64 `json:"completion_rate_percent"`
}

// PayoutListResponse wraps a paginated list of payouts.
type PayoutListResponse struct {
	Payouts    []Payout `json:"payouts"`
	TotalCount int      `json:"total_count"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
}

// IsRetryable returns true if the failure reason is transient.
func IsRetryable(reason string) bool {
	switch reason {
	case FailureBankTimeout, FailureRateLimited, FailureInsufficientFunds:
		return true
	default:
		return false
	}
}
