package server

import (
	"github.com/gin-gonic/gin"

	"zest/pkg/common"
)

// setupZendeskRoutes sets up routes for zendesk-endpoints
func setupZendeskRoutes(r *gin.RouterGroup, s *HTTPServer) {
	r.PUT("/tickets/:"+common.TicketID+"/tags", s.Handlers.ZendeskHandler.UpdateTicketTags)
}
