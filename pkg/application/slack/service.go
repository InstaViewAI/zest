package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"zest/pkg/contracts"
	"zest/pkg/infrastructure/config"
)

// Service talks to the Slack Web API.
type Service struct {
	baseURL string
	token   string
	client  *http.Client

	channelID     string
	botName       string
	reportBotName string
	iconURL       string
}

// PostMessageRequest describes one chat.postMessage call.
type PostMessageRequest struct {
	// Channel defaults to the configured escalation channel when empty.
	Channel string
	Text    string
	// ThreadTS, when set, posts the message as a reply in that thread.
	ThreadTS string
	// Username overrides the bot display name for this message.
	Username string
}

// PostMessageResult carries the identifiers that become the join key stored on
// the Zendesk ticket.
type PostMessageResult struct {
	Channel string
	TS      string
}

type postMessagePayload struct {
	Channel  string `json:"channel"`
	Text     string `json:"text"`
	ThreadTS string `json:"thread_ts,omitempty"`
	Username string `json:"username,omitempty"`
	IconURL  string `json:"icon_url,omitempty"`
}

type postMessageResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

func NewService(cfg config.SlackConfig) *Service {
	return &Service{
		baseURL:       cfg.BaseURL,
		token:         cfg.Token,
		client:        &http.Client{Timeout: cfg.Timeout},
		channelID:     cfg.ChannelID,
		botName:       cfg.BotName,
		reportBotName: cfg.ReportBotName,
		iconURL:       cfg.IconURL,
	}
}

// DefaultChannel is the configured escalation channel.
func (s *Service) DefaultChannel() string { return s.channelID }

// ReportBotName is the display name used for scheduled digests.
func (s *Service) ReportBotName() string { return s.reportBotName }

// PostMessage posts to a channel or, when ThreadTS is set, into a thread.
func (s *Service) PostMessage(ctx context.Context, req PostMessageRequest) (*PostMessageResult, error) {
	channel := req.Channel
	if channel == "" {
		channel = s.channelID
	}

	username := req.Username
	if username == "" {
		username = s.botName
	}

	payload := postMessagePayload{
		Channel:  channel,
		Text:     req.Text,
		ThreadTS: req.ThreadTS,
		Username: username,
		IconURL:  s.iconURL,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, contracts.Internal("failed to encode payload", err)
	}

	url := fmt.Sprintf("%s/chat.postMessage", s.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, contracts.Internal("failed to create request", err)
	}
	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set("Authorization", "Bearer "+s.token)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, contracts.BadGateway("failed to reach Slack API", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Slack answers 200 even for logical failures, so the body decides.
	var apiResp postMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, contracts.BadGateway("failed to decode Slack response", err)
	}

	if !apiResp.OK {
		return nil, contracts.BadGateway(fmt.Sprintf("slack api error: %s", apiResp.Error), nil)
	}

	return &PostMessageResult{Channel: apiResp.Channel, TS: apiResp.TS}, nil
}
