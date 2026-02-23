# Kaveri Market — Batch Payout Processing Engine

A reliable, resumable batch payout processing engine built in **Go** with **PostgreSQL**.

Solves Kaveri Market's payout bottleneck: processes thousands of vendor payouts concurrently, handles failures gracefully, and can be stopped and resumed without duplicate payments.

## Architecture

```
┌──────────────┐     ┌──────────────┐     ┌────────────────┐     ┌──────────────┐
│   REST API   │────▶│   Handlers   │────▶│  Worker Pool   │────▶│  Simulated   │
│  (Gin HTTP)  │     │  (6 routes)  │     │ (N concurrent) │     │  Bank API    │
└──────────────┘     └──────────────┘     └────────────────┘     └──────────────┘
                            │                      │
                            └──────────┬───────────┘
                                       ▼
                              ┌──────────────────┐
                              │   PostgreSQL DB   │
                              │  (State Machine)  │
                              └──────────────────┘
```

### Key Design Decisions

| Decision | Why |
|----------|-----|
| **DB-driven state machine** | Each payout has a status (`pending → processing → completed/failed`). Resumability comes from querying unfinished payouts, not from in-memory cursors. |
| **Claim-before-process** | Workers atomically transition payouts to `processing` before executing. Prevents double-processing even with concurrent workers. |
| **Idempotency via unique key** | `vendor_id:batch_id` is a UNIQUE constraint. The same vendor can't appear twice in a batch, and retries are safe. |
| **Crash recovery on resume** | On startup/resume, any payouts stuck in `processing` are reset to `pending` and retried safely. |
| **Chunked processing** | Payouts are fetched in configurable chunks to avoid loading everything into memory. |
| **Automatic retries** | Retryable failures (timeout, rate limit, insufficient funds) are retried up to 3 times before being marked as permanently failed. |

## Project Structure

```
coding-challenge/
├── cmd/server/main.go              # Entry point, config, DB setup
├── internal/
│   ├── api/
│   │   ├── handlers.go             # HTTP request handlers (6 endpoints)
│   │   └── router.go               # Route definitions
│   ├── models/models.go            # Data models, constants, request/response types
│   ├── repository/repository.go    # All database operations
│   ├── service/simulator.go        # Simulated bank API with realistic outcomes
│   └── worker/
│       ├── pool.go                 # Concurrent worker pool with resumability
│       └── pool_test.go            # Integration tests
├── migrations/001_init.sql         # PostgreSQL schema (3 tables)
├── scripts/
│   ├── seed.go                     # Test data generator (3 batches: 100, 1K, 5K)
│   └── demo.sh                     # Interactive demo script
├── docker-compose.yml              # One-command setup (PostgreSQL + App)
├── Dockerfile                      # Multi-stage Go build
├── Makefile                        # Build shortcuts
└── README.md
```

## Quick Start

### Option 1: Docker Compose (recommended)
```bash
docker-compose up --build -d
```

### Option 2: Local Development
```bash
# Start PostgreSQL
docker run -d --name kaveri-db \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=kaveri_payouts \
  -p 5432:5432 postgres:15-alpine

# Run migrations
psql -h localhost -U postgres -d kaveri_payouts -f migrations/001_init.sql

# Download dependencies and run
go mod tidy
make run
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/batches` | Create a new batch of payouts |
| `GET` | `/api/v1/batches/:id` | Get batch status with summary statistics |
| `POST` | `/api/v1/batches/:id/start` | Start or resume processing a batch |
| `POST` | `/api/v1/batches/:id/stop` | Gracefully stop processing |
| `GET` | `/api/v1/batches/:id/payouts` | List payouts (filter by `?status=failed&page=1&page_size=50`) |
| `POST` | `/api/v1/batches/:id/retry-failed` | Retry all retryable failed payouts |
| `GET` | `/health` | Health check |

## Test Data

The seed script generates **3 batches** with realistic Southeast Asian marketplace data:

```bash
go run scripts/seed.go
```

This creates:
- **Small batch**: 100 payouts (quick validation)
- **Medium batch**: 1,000 payouts (standard processing)
- **Large batch**: 5,000 payouts (NOT auto-started — reserved for resumability demo)

Each payout includes:
- **Vendor ID**: Region-prefixed with category (e.g., `KV-ID-cra-00042`)
- **Vendor name**: Descriptive with region
- **Bank account**: Masked format (e.g., `ID****4521`)
- **Bank name**: Real Southeast Asian banks (BCA, Mandiri, BDO, Vietcombank, etc.)
- **Amount + Currency**: Realistic ranges per currency (IDR, PHP, VND)
- **Transaction IDs**: 1-5 accumulated sale transaction references per vendor

### Failure Distribution (Simulated)

| Outcome | Probability | Retryable | Description |
|---------|-------------|-----------|-------------|
| ✅ Success | 85% | — | Transfer completed |
| ❌ Invalid Bank Account | 5% | No | Permanent — bad bank details |
| ❌ Bank API Timeout | 3% | Yes | Transient — retried automatically |
| ❌ Insufficient Funds | 3% | Yes | Transient — retried automatically |
| ❌ Account Blocked | 2% | No | Permanent — vendor suspended |
| ❌ Rate Limited | 2% | Yes | Transient — retried automatically |

## Demo: Full Walkthrough

### Interactive Demo Script
```bash
bash scripts/demo.sh
```

This walks through all acceptance criteria interactively.

### Manual Demo

#### 1. Create a batch
```bash
curl -X POST http://localhost:8080/api/v1/batches \
  -H "Content-Type: application/json" \
  -d '{
    "payouts": [
      {
        "vendor_id": "KV-ID-001",
        "vendor_name": "Bali Crafts Shop",
        "amount": 2500000,
        "currency": "IDR",
        "bank_account": "ID****7823",
        "bank_name": "BCA",
        "transaction_ids": ["TXN-001-A", "TXN-001-B", "TXN-001-C"]
      },
      {
        "vendor_id": "KV-PH-002",
        "vendor_name": "Manila Electronics Hub",
        "amount": 15750.50,
        "currency": "PHP",
        "bank_account": "PH****3341",
        "bank_name": "BDO",
        "transaction_ids": ["TXN-002-A"]
      }
    ]
  }'
```

#### 2. Start processing
```bash
curl -X POST http://localhost:8080/api/v1/batches/{batch_id}/start
```

#### 3. Monitor progress (during processing)
```bash
# Summary statistics
curl http://localhost:8080/api/v1/batches/{batch_id}

# Example response:
# {
#   "batch": { "status": "in_progress", "total_count": 5000, ... },
#   "statistics": {
#     "total": 5000,
#     "completed": 3241,
#     "failed": 412,
#     "pending": 1347,
#     "processing": 0,
#     "success_rate_percent": 64.82,
#     "completion_rate_percent": 73.06
#   }
# }
```

#### 4. Inspect failures
```bash
curl "http://localhost:8080/api/v1/batches/{batch_id}/payouts?status=failed&page=1&page_size=10"
```

#### 5. Demonstrate resumability
```bash
# Start the large batch
curl -X POST http://localhost:8080/api/v1/batches/{batch_id}/start

# Check partial progress
curl http://localhost:8080/api/v1/batches/{batch_id}
# → Shows e.g. 2,100/5,000 processed

# Kill the server (Ctrl+C or kill the process)

# Restart the server
make run

# Resume — picks up where it left off
curl -X POST http://localhost:8080/api/v1/batches/{batch_id}/start

# Verify completion — total should equal 5,000 with NO duplicates
curl http://localhost:8080/api/v1/batches/{batch_id}
# → completed + failed = 5,000
```

#### 6. Retry failed payouts
```bash
curl -X POST http://localhost:8080/api/v1/batches/{batch_id}/retry-failed
# → {"message": "Retrying failed payouts", "requeued": 47}
```

## Acceptance Criteria Verification

| Criteria | Status | Evidence |
|----------|--------|----------|
| ✅ Batch submitted and processed automatically | Pass | `POST /batches` + `POST /batches/:id/start` |
| ✅ Individual failures recorded with reasons, don't block processing | Pass | `failure_reason` field, isolated goroutines per payout |
| ✅ Stop mid-batch and resume without duplicates | Pass | `POST /stop` + `POST /start`, `ResetStuckProcessing()`, unique idempotency key |
| ✅ API exposes batch status, payout statuses, summary stats | Pass | 6 API endpoints with filtering and pagination |
| ✅ Demonstrable with test data | Pass | `seed.go` (3 batches) + `demo.sh` (interactive) |
| ✅ Runnable with clear setup instructions | Pass | Docker Compose one-liner or local setup steps |

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | Database user |
| `DB_PASSWORD` | `postgres` | Database password |
| `DB_NAME` | `kaveri_payouts` | Database name |
| `SERVER_PORT` | `8080` | HTTP server port |
| `WORKER_CONCURRENCY` | `10` | Number of concurrent workers |
| `WORKER_CHUNK_SIZE` | `100` | Payouts fetched per chunk |

## Running Tests

```bash
# Create test database
createdb -h localhost -U postgres kaveri_payouts_test
psql -h localhost -U postgres -d kaveri_payouts_test -f migrations/001_init.sql

# Run integration tests
go test ./internal/worker/ -v -count=1
```

Tests cover:
- **TestBatchProcessingCompletesAll**: All payouts are processed (completed or failed)
- **TestIdempotency**: Running same batch twice doesn't create duplicate payments
- **TestResumability**: Interrupted batch resumes correctly without data loss
