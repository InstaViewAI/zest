package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SupportHandler serves the liveness endpoints.
type SupportHandler struct {
	serviceName string
}

func NewSupportHandler(serviceName string) *SupportHandler {
	return &SupportHandler{serviceName: serviceName}
}

// Ping godoc: GET /ping
func (h *SupportHandler) Ping(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "service": h.serviceName})
}
