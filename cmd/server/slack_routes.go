package server

import "github.com/gin-gonic/gin"

// setupSlackRoutes sets up routes for slack-endpoints
func setupSlackRoutes(r *gin.RouterGroup, s *HTTPServer) {
	r.POST("/messages", s.Handlers.SlackHandler.PostMessage)
}
