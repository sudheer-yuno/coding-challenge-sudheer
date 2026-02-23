package api

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"coding-challenge/internal/models"
	"coding-challenge/internal/repository"
	"coding-challenge/internal/worker"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Handler holds dependencies for API handlers.
type Handler struct {
	repo *repository.Repository
	pool *worker.Pool
}

// NewHandler creates a new handler with dependencies.
func NewHandler(repo *repository.Repository, pool *worker.Pool) *Handler {
	return &Handler{repo: repo, pool: pool}
}

// CreateBatch creates a new batch of payouts.
// POST /api/v1/batches
func (h *Handler) CreateBatch(c *gin.Context) {
	var req models.CreateBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	batch, err := h.repo.CreateBatch(c.Request.Context(), req.Payouts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create batch: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Batch created successfully",
		"batch_id": batch.ID,
		"total":    batch.TotalCount,
		"status":   batch.Status,
	})
}

// StartBatch begins or resumes processing a batch.
// POST /api/v1/batches/:id/start
func (h *Handler) StartBatch(c *gin.Context) {
	batchID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid batch ID"})
		return
	}

	batch, err := h.repo.GetBatch(c.Request.Context(), batchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if batch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Batch not found"})
		return
	}

	if h.pool.IsRunning() {
		c.JSON(http.StatusConflict, gin.H{"error": "A batch is already being processed"})
		return
	}

	// Start processing in background
	go func() {
		ctx := context.Background()
		if err := h.pool.ProcessBatch(ctx, batchID); err != nil {
			log.Printf("[api] Error processing batch %s: %v", batchID, err)
		}
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "Batch processing started",
		"batch_id": batchID,
	})
}

// StopBatch stops processing a batch (graceful).
// POST /api/v1/batches/:id/stop
func (h *Handler) StopBatch(c *gin.Context) {
	h.pool.Stop()
	c.JSON(http.StatusOK, gin.H{"message": "Stop signal sent. Processing will pause after current chunk."})
}

// GetBatch returns batch status with statistics.
// GET /api/v1/batches/:id
func (h *Handler) GetBatch(c *gin.Context) {
	batchID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid batch ID"})
		return
	}

	batch, err := h.repo.GetBatch(c.Request.Context(), batchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if batch == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Batch not found"})
		return
	}

	stats, err := h.repo.GetBatchStatistics(c.Request.Context(), batchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.BatchSummary{
		Batch:      *batch,
		Statistics: *stats,
	})
}

// GetBatchPayouts returns paginated payouts for a batch with optional status filter.
// GET /api/v1/batches/:id/payouts?status=failed&page=1&page_size=50
func (h *Handler) GetBatchPayouts(c *gin.Context) {
	batchID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid batch ID"})
		return
	}

	status := c.Query("status")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	payouts, total, err := h.repo.GetPayoutsByBatch(c.Request.Context(), batchID, status, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.PayoutListResponse{
		Payouts:    payouts,
		TotalCount: total,
		Page:       page,
		PageSize:   pageSize,
	})
}

// RetryFailed retries all retryable failed payouts and restarts processing.
// POST /api/v1/batches/:id/retry-failed
func (h *Handler) RetryFailed(c *gin.Context) {
	batchID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid batch ID"})
		return
	}

	requeued, err := h.repo.RetryFailedPayouts(c.Request.Context(), batchID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if requeued == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "No retryable payouts found"})
		return
	}

	// Start processing again
	go func() {
		ctx := context.Background()
		if err := h.pool.ProcessBatch(ctx, batchID); err != nil {
			log.Printf("[api] Error retrying batch %s: %v", batchID, err)
		}
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "Retrying failed payouts",
		"requeued": requeued,
	})
}
