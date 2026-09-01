package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"zest/pkg/application/escalation"
	"zest/pkg/contracts"
	"zest/pkg/infrastructure/config"
)

// EscalationHandler serves the Zendesk-driven webhook endpoints and the manual
// report trigger.
type EscalationHandler struct {
	service   *escalation.Service
	report    config.ReportConfig
	subdomain string
}

func NewEscalationHandler(service *escalation.Service, report config.ReportConfig, subdomain string) *EscalationHandler {
	return &EscalationHandler{service: service, report: report, subdomain: subdomain}
}

// Escalate godoc: POST /api/v1/webhooks/zendesk/escalation
func (h *EscalationHandler) Escalate(c *gin.Context) {
	var payload contracts.EscalationWebhook
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
		return
	}

	res, err := h.service.HandleEscalation(c.Request.Context(), payload)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

// Comment godoc: POST /api/v1/webhooks/zendesk/comment
func (h *EscalationHandler) Comment(c *gin.Context) {
	var payload contracts.CommentWebhook
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
		return
	}

	res, err := h.service.HandleComment(c.Request.Context(), payload)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

// Status godoc: POST /api/v1/webhooks/zendesk/status
func (h *EscalationHandler) Status(c *gin.Context) {
	var payload contracts.StatusWebhook
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
		return
	}

	res, err := h.service.HandleStatusChange(c.Request.Context(), payload)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

// RunReport godoc: POST /api/v1/reports/stale
// Runs the same digest the scheduler posts, on demand — useful for verifying
// the report renders correctly without waiting for Saturday.
func (h *EscalationHandler) RunReport(c *gin.Context) {
	res, err := h.service.PostStaleReport(c.Request.Context(),
		"Escalation Report", h.report.StaleAfter, h.subdomain)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}
