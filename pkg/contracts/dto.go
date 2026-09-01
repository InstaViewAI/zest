package contracts

// SlackMessageRequest is the payload for POST /slack/message.
type SlackMessageRequest struct {
	Channel string `json:"channel" binding:"required"`
	Message string `json:"message" binding:"required"`
}

// ZendeskUpdateRequest is the payload for PUT /zendesk/tickets/:ticket_id/tags.
type ZendeskUpdateRequest struct {
	Tags []string `json:"tags" binding:"required,min=1,dive,required"`
}

// ZendeskTagsResponse is returned after a successful tag update.
type ZendeskTagsResponse struct {
	Message string   `json:"message"`
	Tags    []string `json:"tags"`
}

// MessageResponse is a generic acknowledgement body.
type MessageResponse struct {
	Message string `json:"message"`
}

// ErrorResponse is the uniform error body for every endpoint.
type ErrorResponse struct {
	Error string `json:"error"`
}
