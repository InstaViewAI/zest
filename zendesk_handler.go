package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type ZendeskUpdateRequest struct {
	TicketID string   `json:"ticket_id" binding:"required"`
	Tags     []string `json:"tags" binding:"required"`
}

type ZendeskTagsPayload struct {
	Tags []string `json:"tags"`
}

func updateZendeskTicket(c *gin.Context) {
	var req ZendeskUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	subdomain := "instaviewhomesupport"
	email := "mansi.patil@instaview.ai"
	apiToken := "your_zendesk_api_token"

	payload := ZendeskTagsPayload{Tags: req.Tags}
	body, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode payload"})
		return
	}

	url := fmt.Sprintf("https://%s.zendesk.com/api/v2/tickets/%s/tags", subdomain, req.TicketID)
	zdReq, err := http.NewRequest("PUT", url, bytes.NewBuffer(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}
	zdReq.Header.Set("Content-Type", "application/json")
	zdReq.SetBasicAuth(email+"/token", apiToken)

	client := &http.Client{}
	resp, err := client.Do(zdReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to reach Zendesk API"})
		return
	}
	defer resp.Body.Close()

	var zdResp map[string]any
	json.NewDecoder(resp.Body).Decode(&zdResp)

	if resp.StatusCode != http.StatusOK {
		c.JSON(resp.StatusCode, gin.H{"error": zdResp})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Tags updated successfully", "tags": zdResp["tags"]})
}
