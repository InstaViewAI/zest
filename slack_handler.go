package main

import (
	"bytes"
	"encoding/json"
	"net/http"

	// "os"

	"github.com/gin-gonic/gin"
)

type SlackMessageRequest struct {
	Channel string `json:"channel" binding:"required"`
	Message string `json:"message" binding:"required"`
}

type SlackAPIPayload struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

func postSlackMessage(c *gin.Context) {
	var req SlackMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// token := os.Getenv("SLACK_BOT_TOKEN")
	// if token == "" {
	// 	c.JSON(http.StatusInternalServerError, gin.H{"error": "SLACK_BOT_TOKEN not set"})
	// 	return
	// }
	token := "xoxb-your-slack-bot-token"
	payload := SlackAPIPayload{
		Channel: req.Channel,
		Text:    req.Message,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode payload"})
		return
	}

	slackReq, err := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", bytes.NewBuffer(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}
	slackReq.Header.Set("Content-Type", "application/json")
	slackReq.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(slackReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to reach Slack API"})
		return
	}
	defer resp.Body.Close()

	var slackResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&slackResp)

	if ok, _ := slackResp["ok"].(bool); !ok {
		c.JSON(http.StatusBadGateway, gin.H{"error": slackResp["error"]})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Message posted successfully"})
}
