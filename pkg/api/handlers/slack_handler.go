package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"zest/pkg/application/slack"
	"zest/pkg/contracts"
)

// SlackHandler exposes ad-hoc Slack posting over HTTP.
type SlackHandler struct {
	service *slack.Service
}

func NewSlackHandler(service *slack.Service) *SlackHandler {
	return &SlackHandler{service: service}
}

// PostMessage godoc: POST /api/v1/slack/messages
func (h *SlackHandler) PostMessage(c *gin.Context) {
	var req contracts.SlackMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
		return
	}

	if _, err := h.service.PostMessage(c.Request.Context(), slack.PostMessageRequest{
		Channel: req.Channel,
		Text:    req.Message,
	}); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, contracts.MessageResponse{Message: "Message posted successfully"})
}
