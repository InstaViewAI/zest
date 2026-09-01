package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"zest/pkg/application/zendesk"
	"zest/pkg/common"
	"zest/pkg/contracts"
)

// ZendeskHandler exposes the Zendesk integration over HTTP.
type ZendeskHandler struct {
	service *zendesk.Service
}

func NewZendeskHandler(service *zendesk.Service) *ZendeskHandler {
	return &ZendeskHandler{service: service}
}

// UpdateTicketTags godoc: PUT /zendesk/tickets/:ticket_id/tags
func (h *ZendeskHandler) UpdateTicketTags(c *gin.Context) {
	ticketID := c.Param(common.TicketID)
	if ticketID == "" {
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "ticket_id is required"})
		return
	}

	var req contracts.ZendeskUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
		return
	}

	tags, err := h.service.UpdateTicketTags(c.Request.Context(), ticketID, req.Tags)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, contracts.ZendeskTagsResponse{
		Message: "Tags updated successfully",
		Tags:    tags,
	})
}
