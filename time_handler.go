package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type TimeResponse struct {
	Time     string `json:"time"`
	UnixTime int64  `json:"unix_time"`
	TimeZone string `json:"timezone"`
}

func getCurrentTime(c *gin.Context) {
	now := time.Now()
	zone, _ := now.Zone()

	c.JSON(http.StatusOK, TimeResponse{
		Time:     now.Format(time.RFC3339),
		UnixTime: now.Unix(),
		TimeZone: zone,
	})
}
