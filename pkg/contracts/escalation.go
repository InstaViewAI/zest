package contracts

// EscalationWebhook is the payload Zendesk posts when a ticket is escalated.
// Field names mirror the Catch Hook payload the Zapier build expected, with
// snake_case aliases so a Zendesk webhook body can use either convention.
type EscalationWebhook struct {
	TicketID      string `json:"ticket_id" binding:"required"`
	TicketURL     string `json:"ticket_url"`
	Subject       string `json:"subject"`
	Status        string `json:"status"`
	Priority      string `json:"priority"`
	Brand         string `json:"brand"`
	RequesterName string `json:"requester_name"`
	RequesterMail string `json:"requester_email"`
	AgentName     string `json:"agent_name"`
	AgentMail     string `json:"agent_email"`
	DeviceType    string `json:"device_type"`
	DeviceModel   string `json:"device_model"`
	DeviceIDs     string `json:"device_ids"`
	DescribeIssue string `json:"describe_issue"`
	Description   string `json:"description"`
	Notes         string `json:"notes"`
}

// CommentWebhook is the payload Zendesk posts when a comment is added to an
// already-escalated ticket. ChannelID and ThreadTS are the join key; when the
// Zendesk trigger doesn't send them they are recovered from the ticket.
type CommentWebhook struct {
	TicketID  string `json:"ticket_id" binding:"required"`
	Comment   string `json:"comment" binding:"required"`
	Author    string `json:"author"`
	Status    string `json:"status"`
	ChannelID string `json:"channel_id"`
	ThreadTS  string `json:"ts"`
}

// StatusWebhook is the payload Zendesk posts when a ticket's status changes.
type StatusWebhook struct {
	TicketID  string `json:"ticket_id" binding:"required"`
	Status    string `json:"status" binding:"required"`
	Previous  string `json:"previous_status"`
	Assignee  string `json:"assignee"`
	ChannelID string `json:"channel_id"`
	ThreadTS  string `json:"ts"`
}

// EscalationResponse is returned after a thread has been created.
type EscalationResponse struct {
	Message   string `json:"message"`
	TicketID  string `json:"ticket_id"`
	ChannelID string `json:"channel_id"`
	ThreadTS  string `json:"thread_ts"`
}

// WebhookResponse acknowledges a sync webhook. Skipped is true when the ticket
// had no Slack thread and the update was intentionally dropped.
type WebhookResponse struct {
	Message  string `json:"message"`
	TicketID string `json:"ticket_id"`
	Skipped  bool   `json:"skipped"`
}

// ReportResponse is returned by the manual report-trigger endpoint.
type ReportResponse struct {
	Message string `json:"message"`
	Tickets int    `json:"tickets"`
	Posted  bool   `json:"posted"`
}
