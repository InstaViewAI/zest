package zendesk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"zest/pkg/contracts"
	"zest/pkg/infrastructure/config"
)

// maxSearchPages bounds pagination so a runaway query can't loop forever.
const maxSearchPages = 20

// Service talks to the Zendesk Support API.
type Service struct {
	baseURL  string
	email    string
	apiToken string
	client   *http.Client
}

// CustomField is one entry of a ticket's custom_fields array.
type CustomField struct {
	ID    int64 `json:"id"`
	Value any   `json:"value"`
}

// Ticket is the subset of the Zendesk ticket model this service reads.
type Ticket struct {
	ID           int64         `json:"id"`
	Subject      string        `json:"subject"`
	Status       string        `json:"status"`
	Priority     string        `json:"priority"`
	Tags         []string      `json:"tags"`
	UpdatedAt    time.Time     `json:"updated_at"`
	CustomFields []CustomField `json:"custom_fields"`
}

// CustomFieldValue returns the string value of a custom field by id.
func (t *Ticket) CustomFieldValue(id int64) string {
	for _, f := range t.CustomFields {
		if f.ID != id {
			continue
		}
		if s, ok := f.Value.(string); ok {
			return s
		}
	}

	return ""
}

// Comment is an update note written onto a ticket.
type Comment struct {
	Body   string `json:"body"`
	Public bool   `json:"public"`
}

// TicketUpdate is the mutable part of a ticket update request. Tags are absent
// on purpose: additional_tags only exists on the bulk "Update Many Tickets"
// endpoint, and a single-ticket update silently discards it. Use AppendTags.
type TicketUpdate struct {
	CustomFields []CustomField `json:"custom_fields,omitempty"`
	Comment      *Comment      `json:"comment,omitempty"`
	Status       string        `json:"status,omitempty"`
}

type ticketEnvelope struct {
	Ticket TicketUpdate `json:"ticket"`
}

type ticketResponse struct {
	Ticket Ticket `json:"ticket"`
}

type tagsPayload struct {
	Tags []string `json:"tags"`
}

type searchResponse struct {
	Results  []Ticket `json:"results"`
	NextPage string   `json:"next_page"`
}

func NewService(cfg config.ZendeskConfig) *Service {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s.zendesk.com/api/v2", cfg.Subdomain)
	}

	return &Service{
		baseURL:  baseURL,
		email:    cfg.Email,
		apiToken: cfg.APIToken,
		client:   &http.Client{Timeout: cfg.Timeout},
	}
}

// UpdateTicketTags adds tags to a ticket and returns the resulting set. Zendesk
// maps PUT on this endpoint to "add tags", so existing tags are kept; POST is
// the one that replaces them.
func (s *Service) UpdateTicketTags(ctx context.Context, ticketID string, tags []string) ([]string, error) {
	var out tagsPayload

	endpoint := fmt.Sprintf("%s/tickets/%s/tags", s.baseURL, url.PathEscape(ticketID))
	if err := s.do(ctx, http.MethodPut, endpoint, tagsPayload{Tags: tags}, &out); err != nil {
		return nil, err
	}

	return out.Tags, nil
}

// UpdateTicket applies a partial update — tags, custom fields, a comment — and
// returns the ticket as Zendesk stored it.
func (s *Service) UpdateTicket(ctx context.Context, ticketID string, update TicketUpdate) (*Ticket, error) {
	var out ticketResponse

	endpoint := fmt.Sprintf("%s/tickets/%s.json", s.baseURL, url.PathEscape(ticketID))
	if err := s.do(ctx, http.MethodPut, endpoint, ticketEnvelope{Ticket: update}, &out); err != nil {
		return nil, err
	}

	return &out.Ticket, nil
}

// GetTicket fetches a single ticket, used to recover the Slack join key when a
// webhook payload doesn't carry it.
func (s *Service) GetTicket(ctx context.Context, ticketID string) (*Ticket, error) {
	var out ticketResponse

	endpoint := fmt.Sprintf("%s/tickets/%s.json", s.baseURL, url.PathEscape(ticketID))
	if err := s.do(ctx, http.MethodGet, endpoint, nil, &out); err != nil {
		return nil, err
	}

	return &out.Ticket, nil
}

// SearchTickets runs a Zendesk search query, following pagination.
func (s *Service) SearchTickets(ctx context.Context, query string) ([]Ticket, error) {
	endpoint := fmt.Sprintf("%s/search.json?query=%s", s.baseURL, url.QueryEscape(query))

	tickets := []Ticket{}
	for page := 0; endpoint != "" && page < maxSearchPages; page++ {
		var out searchResponse
		if err := s.do(ctx, http.MethodGet, endpoint, nil, &out); err != nil {
			return nil, err
		}

		tickets = append(tickets, out.Results...)
		endpoint = out.NextPage
	}

	return tickets, nil
}

// do performs one authenticated request, encoding body and decoding into out
// when they are non-nil, and maps transport failures onto contracts.Error.
func (s *Service) do(ctx context.Context, method, endpoint string, body, out any) error {
	var reader io.Reader
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return contracts.Internal("failed to encode payload", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return contracts.Internal("failed to create request", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth(s.email+"/token", s.apiToken)

	// TEMP DEBUG: remove once the ticket write-back is understood.
	log.Printf("[zendesk-debug] --> %s %s\n[zendesk-debug]     auth-user=%q token-len=%d\n[zendesk-debug]     body=%s",
		method, endpoint, s.email, len(s.apiToken), string(encoded))

	resp, err := s.client.Do(req)
	if err != nil {
		return contracts.BadGateway("failed to reach Zendesk API", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// TEMP DEBUG: resp.Request.URL is the URL actually served, so it differs
	// from the one above whenever the client followed a redirect.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	log.Printf("[zendesk-debug] <-- %d final-url=%s\n[zendesk-debug]     body=%s",
		resp.StatusCode, resp.Request.URL.String(), string(raw))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return contracts.NewError(resp.StatusCode, fmt.Sprintf("zendesk api error: %s", string(raw)), nil)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(out); err != nil {
		return contracts.BadGateway("failed to decode Zendesk response", err)
	}

	return nil
}
