.PHONY: build run test seed docker-up docker-down migrate

# Build the binary
build:
	go build -o bin/payout-engine ./cmd/server

# Run locally
run: build
	./bin/payout-engine

# Run with Docker Compose
docker-up:
	docker-compose up --build -d

docker-down:
	docker-compose down -v

# Run migrations manually (requires psql)
migrate:
	psql -h localhost -U postgres -d kaveri_payouts -f migrations/001_init.sql

# Seed test data: create a batch of 1000 payouts
seed:
	@echo "Creating batch with 1000 payouts..."
	@go run scripts/seed.go

# Run tests
test:
	go test ./... -v -count=1
