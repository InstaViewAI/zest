package server

import (
	"zest/pkg/middleware"
)

// SetupRoutes registers every route group on the engine.
func (s *HTTPServer) SetupRoutes() {
	setupSupportRoutes(s)

	root := s.Engine.Group(BasePath)

	// API groups
	slackGroup := root.Group("/slack")
	zendeskGroup := root.Group("/zendesk")
	reportGroup := root.Group("/reports")

	// Inbound Zendesk webhooks are signature-verified as a group.
	webhookGroup := root.Group("/webhooks/zendesk",
		middleware.VerifyZendeskSignature(s.Config.Zendesk.WebhookSecret))

	// register handler functions
	setupSlackRoutes(slackGroup, s)
	setupZendeskRoutes(zendeskGroup, s)
	setupWebhookRoutes(webhookGroup, s)
	setupReportRoutes(reportGroup, s)
}

// setupSupportRoutes registers the liveness endpoints outside the versioned
// base path so probes never have to follow an API version bump.
func setupSupportRoutes(s *HTTPServer) {
	s.Engine.GET("/ping", s.Handlers.SupportHandler.Ping)
	s.Engine.GET(BasePath+"/ping", s.Handlers.SupportHandler.Ping)
}
