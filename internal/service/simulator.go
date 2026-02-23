package service

import (
	"math/rand"
	"time"

	"coding-challenge/internal/models"
)

// SimulatedBankResult represents the outcome of a simulated bank transfer.
type SimulatedBankResult struct {
	Success      bool
	FailureCode  string
	IsRetryable  bool
	LatencyMs    int
}

// SimulateBankTransfer simulates calling a bank API to transfer funds.
// Realistic distribution:
//   - 85% success
//   - 5% INVALID_BANK_ACCOUNT (permanent)
//   - 3% BANK_API_TIMEOUT (retryable)
//   - 3% INSUFFICIENT_FUNDS (retryable)
//   - 2% ACCOUNT_BLOCKED (permanent)
//   - 2% RATE_LIMITED (retryable)
func SimulateBankTransfer(vendorID string, amount float64) SimulatedBankResult {
	// Simulate network latency: 50-500ms
	latency := 50 + rand.Intn(450)
	time.Sleep(time.Duration(latency) * time.Millisecond)

	roll := rand.Float64() * 100

	switch {
	case roll < 85:
		return SimulatedBankResult{
			Success:   true,
			LatencyMs: latency,
		}
	case roll < 90:
		return SimulatedBankResult{
			Success:     false,
			FailureCode: models.FailureInvalidBankAccount,
			IsRetryable: false,
			LatencyMs:   latency,
		}
	case roll < 93:
		return SimulatedBankResult{
			Success:     false,
			FailureCode: models.FailureBankTimeout,
			IsRetryable: true,
			LatencyMs:   latency,
		}
	case roll < 96:
		return SimulatedBankResult{
			Success:     false,
			FailureCode: models.FailureInsufficientFunds,
			IsRetryable: true,
			LatencyMs:   latency,
		}
	case roll < 98:
		return SimulatedBankResult{
			Success:     false,
			FailureCode: models.FailureAccountBlocked,
			IsRetryable: false,
			LatencyMs:   latency,
		}
	default:
		return SimulatedBankResult{
			Success:     false,
			FailureCode: models.FailureRateLimited,
			IsRetryable: true,
			LatencyMs:   latency,
		}
	}
}
