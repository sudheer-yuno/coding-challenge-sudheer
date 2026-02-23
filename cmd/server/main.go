package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"

	"coding-challenge/internal/api"
	"coding-challenge/internal/repository"
	"coding-challenge/internal/worker"

	_ "github.com/lib/pq"
)

func main() {
	// Configuration from environment variables
	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPass := getEnv("DB_PASSWORD", "postgres")
	dbName := getEnv("DB_NAME", "kaveri_payouts")
	serverPort := getEnv("SERVER_PORT", "8080")
	concurrency, _ := strconv.Atoi(getEnv("WORKER_CONCURRENCY", "10"))
	chunkSize, _ := strconv.Atoi(getEnv("WORKER_CHUNK_SIZE", "100"))

	// Connect to PostgreSQL
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPass, dbName,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Database unreachable: %v", err)
	}
	log.Println("Connected to PostgreSQL")

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Initialize layers
	repo := repository.New(db)
	pool := worker.NewPool(repo, concurrency, chunkSize)
	router := api.SetupRouter(repo, pool)

	// Start server
	addr := ":" + serverPort
	log.Printf("Kaveri Batch Payout Engine starting on %s", addr)
	log.Printf("Config: concurrency=%d, chunk_size=%d", concurrency, chunkSize)
	log.Println("Endpoints:")
	log.Println("  POST   /api/v1/batches              - Create batch")
	log.Println("  GET    /api/v1/batches/:id           - Batch status")
	log.Println("  POST   /api/v1/batches/:id/start     - Start/resume")
	log.Println("  POST   /api/v1/batches/:id/stop      - Stop processing")
	log.Println("  GET    /api/v1/batches/:id/payouts   - List payouts")
	log.Println("  POST   /api/v1/batches/:id/retry-failed - Retry failures")

	if err := router.Run(addr); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
