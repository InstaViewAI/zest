package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"zest/pkg/application/escalation"
	"zest/pkg/application/slack"
	"zest/pkg/application/zendesk"
	"zest/pkg/contracts"
	"zest/pkg/infrastructure/config"
)

// Handlers is the aggregate of every HTTP handler, wired once at boot and
// handed to the router.
type Handlers struct {
	SupportHandler    *SupportHandler
	SlackHandler      *SlackHandler
	ZendeskHandler    *ZendeskHandler
	EscalationHandler *EscalationHandler
}

// Services exposes the application services that outlive request handling —
// the scheduler needs the escalation service too.
type Services struct {
	Slack      *slack.Service
	Zendesk    *zendesk.Service
	Escalation *escalation.Service
}

// NewServices builds the application layer from configuration.
func NewServices(cfg *config.AppConfig) *Services {
	slackSvc := slack.NewService(cfg.Slack)
	zendeskSvc := zendesk.NewService(cfg.Zendesk)

	return &Services{
		Slack:      slackSvc,
		Zendesk:    zendeskSvc,
		Escalation: escalation.NewService(slackSvc, zendeskSvc, cfg.Escalation),
	}
}

// New wires the HTTP handlers on top of the application services.
func New(cfg *config.AppConfig, svc *Services) *Handlers {
	return &Handlers{
		SupportHandler:    NewSupportHandler(cfg.Server.Name),
		SlackHandler:      NewSlackHandler(svc.Slack),
		ZendeskHandler:    NewZendeskHandler(svc.Zendesk),
		EscalationHandler: NewEscalationHandler(svc.Escalation, cfg.Report, cfg.Zendesk.Subdomain),
	}
}

// respondError maps an application error onto the wire, defaulting to 500 for
// anything that isn't a *contracts.Error.
func respondError(c *gin.Context, err error) {
	var appErr *contracts.Error
	if errors.As(err, &appErr) {
		c.JSON(appErr.Status, contracts.ErrorResponse{Error: appErr.Message})
		return
	}

	c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
}
