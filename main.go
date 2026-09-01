package main

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	r.GET("/time", getCurrentTime)
	r.POST("/slack/message", postSlackMessage)
	r.PUT("/zendesk/ticket", updateZendeskTicket)

	fmt.Println("Server running on http://localhost:8080")
	r.Run(":8080")
}
