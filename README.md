# Kaveri Market — Batch Payout Processing Engine

A reliable, resumable batch payout processing engine built in Go with PostgreSQL.

## Architecture

```
┌──────────────┐     ┌──────────────┐     ┌────────────────┐     ┌──────────────┐
│   REST API   │────▶│   Service    │────▶│  Worker Pool   │────▶│  Simulated   │
│  (Gin HTTP)  │     │  (Handlers)  │     │ (N concurrent) │     │  Bank API    │
└──────────────┘     └──────────────┘     └────────────────┘     └──────────────┘
                            │                      │
                            └──────────┬───────────┘
                                       ▼
                              ┌──────────────────┐
                              │   PostgreSQL DB   │
                              │ (State Machine)   │
                              └──────────────────┘
```

### Key Design Decisions

1. **DB-driven state machine**: Each payout has a status (`pending → processing → completed/failed`). Resumability comes from querying unfinshed payouts, not from cursors.

2. **Claim-before-process**: Workers atomically transition payouts to `processing` before executing. This prevents double-processing even with concurrent workers.

3. **Idempotency via unique key**: `vendor_id:batch_id` is a unique constraint. The same vendor can't appear twice in a batch.

4. **Crash recovery**: On startup/resume, any payouts stuck in `processing` are reset to `pending` and retried.

5. **Chunked processing**: Payouts are fetched and processed in configurable chunks to avoid loading everything into memory.

## Project Structure

```
coding-challenge/
├── cmd/server/main.go              # Entry point
├── internal/
│   ├── api/
│   │   ├── handlers.go             # HTTP request handlers
│   │   └── router.go               # Route definitions
│   ├── models/models.go            # Data models & constants
│   ├── repository/repository.go    # Database operations
│   ├── service/simulator.go        # Simulated bank API
│   └── worker/pool.go              # Concurrent worker pool
├── migrations/001_init.sql         # Database schema
├── scripts/seed.go                 # Test data generator
├── docker-compose.yml              # PostgreSQL + App
├── Dockerfile                      # Multi-stage build
├── Makefile                        # Build commands
└── README.md
```

## Quick Start

### Option 1: Docker Compose (recommended)
```bash
docker-compose up --build -d
```

### Option 2: Local
```bash
# Start PostgreSQL
docker run -d --name kaveri-db \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=kaveri_payouts \
  -p 5432:5432 postgres:15-alpine

# Run migrations
psql -h localhost -U postgres -d kaveri_payouts -f migrations/001_init.sql

# Build and run
make run
```

## API Usage

### 1. Create a batch
```bash
curl -X POST http://localhost:8080/api/v1/batches \
  -H "Content-Type: application/json" \
  -d '{
    "payouts": [
      {"vendor_id": "v001", "vendor_name": "Vendor A", "amount": 150.00, "currency": "IDR", "bank_account": "1234567890", "bank_name": "BCA"},
      {"vendor_id": "v002", "vendor_name": "Vendor B", "amount": 250.50, "currency": "PHP", "bank_account": "0987654321", "bank_name": "BDO"}
    ]
  }'
```

### 2. Start/resume processing
```bash
curl -X POST http://localhost:8080/api/v1/batches/{batch_id}/start
```

### 3. Check batch status
```bash
curl http://localhost:8080/api/v1/batches/{batch_id}
```

### 4. View failed payouts
```bash
curl "http://localhost:8080/api/v1/batches/{batch_id}/payouts?status=failed"
```

### 5. Retry failed payouts
```bash
curl -X POST http://localhost:8080/api/v1/batches/{batch_id}/retry-failed
```

### 6. Stop processing (graceful)
```bash
curl -X POST http://localhost:8080/api/v1/batches/{batch_id}/stop
```

## Seed Test Data

Generate 1000 (or custom count) vendor payouts and start processing:
```bash
go run scripts/seed.go        # Default: 1000 payouts
go run scripts/seed.go 5000   # Custom: 5000 payouts
```

## Testing Resumability

```bash
# 1. Seed a large batch
go run scripts/seed.go 5000

# 2. Watch it process
curl http://localhost:8080/api/v1/batches/{id}

# 3. Kill the server mid-processing (Ctrl+C or kill)

# 4. Restart the server
make run

# 5. Resume the batch — it picks up where it left off
curl -X POST http://localhost:8080/api/v1/batches/{id}/start

# 6. Verify no duplicates — completed count should never exceed total
curl http://localhost:8080/api/v1/batches/{id}
```

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

## Simulated Failure Distribution

| Outcome | Probability | Retryable |
|---------|-------------|-----------|
| Success | 85% | — |
| Invalid Bank Account | 5% | No |
| Bank API Timeout | 3% | Yes |
| Insufficient Funds | 3% | Yes |
| Account Blocked | 2% | No |
| Rate Limited | 2% | Yes |

Failed payouts with retryable errors are automatically retried up to 3 times during batch processing.
