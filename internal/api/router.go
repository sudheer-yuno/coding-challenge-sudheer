package api

import (
	"coding-challenge/internal/repository"
	"coding-challenge/internal/worker"

	"github.com/gin-gonic/gin"
)

// SetupRouter creates and configures the Gin router with all routes.
func SetupRouter(repo *repository.Repository, pool *worker.Pool) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	h := NewHandler(repo, pool)

	v1 := r.Group("/api/v1")
	{
		batches := v1.Group("/batches")
		{
			batches.POST("", h.CreateBatch)                  // Create a new batch
			batches.GET("/:id", h.GetBatch)                  // Get batch status + stats
			batches.POST("/:id/start", h.StartBatch)         // Start/resume processing
			batches.POST("/:id/stop", h.StopBatch)           // Stop processing
			batches.GET("/:id/payouts", h.GetBatchPayouts)   // List payouts (filterable)
			batches.POST("/:id/retry-failed", h.RetryFailed) // Retry failed payouts
		}
	}

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return r
}
