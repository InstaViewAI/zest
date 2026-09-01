package server

import (
	"github.com/gin-gonic/gin"
)

// setupWebhookRoutes sets up routes for zendesk-webhook-endpoints. These are
// the entry points that replace the Zapier Catch Hooks, so they sit behind
// signature verification rather than the public API surface.
func setupWebhookRoutes(r *gin.RouterGroup, s *HTTPServer) {
	r.POST("/escalation", s.Handlers.EscalationHandler.Escalate)
	r.POST("/comment", s.Handlers.EscalationHandler.Comment)
	r.POST("/status", s.Handlers.EscalationHandler.Status)
}

// setupReportRoutes sets up routes for report-endpoints
func setupReportRoutes(r *gin.RouterGroup, s *HTTPServer) {
	r.POST("/stale", s.Handlers.EscalationHandler.RunReport)
}
