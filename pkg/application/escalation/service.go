package escalation

import (
	"context"
	"fmt"
	"log"
	"strings"

	"zest/pkg/application/slack"
	"zest/pkg/application/zendesk"
	"zest/pkg/contracts"
	"zest/pkg/infrastructure/config"
)

// commentPreviewLimit truncates long comments so a thread reply stays readable.
const commentPreviewLimit = 300

// Service orchestrates the Zendesk <-> Slack escalation flows. It owns no
// state: the join key (channel id + thread ts) lives on the Zendesk ticket.
type Service struct {
	slack   *slack.Service
	zendesk *zendesk.Service
	cfg     config.EscalationConfig
}

// thread is the resolved Slack destination for a ticket update.
type thread struct {
	channelID string
	ts        string
}

func NewService(slackSvc *slack.Service, zendeskSvc *zendesk.Service, cfg config.EscalationConfig) *Service {
	return &Service{slack: slackSvc, zendesk: zendeskSvc, cfg: cfg}
}

// HandleEscalation posts the escalation alert, then writes the resulting
// thread's channel id and timestamp back onto the ticket along with the
// escalation tag. That write-back is what lets every later flow find the
// thread, so a failure there is reported even though Slack already has the
// message.
func (s *Service) HandleEscalation(ctx context.Context, payload contracts.EscalationWebhook) (*contracts.EscalationResponse, error) {
	posted, err := s.slack.PostMessage(ctx, slack.PostMessageRequest{
		Text: buildEscalationMessage(payload),
	})
	if err != nil {
		return nil, err
	}

	update := zendesk.TicketUpdate{
		CustomFields: []zendesk.CustomField{
			{ID: s.cfg.ThreadTSFieldID, Value: posted.TS},
			{ID: s.cfg.ChannelIDFieldID, Value: posted.Channel},
		},
	}

	if _, err := s.zendesk.UpdateTicket(ctx, payload.TicketID, update); err != nil {
		// The thread exists but the ticket can't point at it. Surfacing the
		// error lets Zendesk retry the webhook rather than silently leaving an
		// orphaned thread that no later update can reach.
		return nil, fmt.Errorf("slack thread %s/%s created but ticket write-back failed: %w",
			posted.Channel, posted.TS, err)
	}

	// The tag needs its own call: a single-ticket update ignores additional_tags.
	if _, err := s.zendesk.UpdateTicketTags(ctx, payload.TicketID, []string{s.cfg.Tag}); err != nil {
		return nil, fmt.Errorf("slack thread %s/%s created and ticket updated but tagging failed: %w",
			posted.Channel, posted.TS, err)
	}

	return &contracts.EscalationResponse{
		Message:   "Escalation posted",
		TicketID:  payload.TicketID,
		ChannelID: posted.Channel,
		ThreadTS:  posted.TS,
	}, nil
}

// HandleComment mirrors a ticket comment into the ticket's Slack thread.
func (s *Service) HandleComment(ctx context.Context, payload contracts.CommentWebhook) (*contracts.WebhookResponse, error) {
	return s.replyInThread(ctx, payload.TicketID, payload.ChannelID, payload.ThreadTS,
		buildCommentMessage(payload))
}

// HandleStatusChange mirrors a status transition into the ticket's thread.
func (s *Service) HandleStatusChange(ctx context.Context, payload contracts.StatusWebhook) (*contracts.WebhookResponse, error) {
	return s.replyInThread(ctx, payload.TicketID, payload.ChannelID, payload.ThreadTS,
		buildStatusMessage(payload))
}

// replyInThread resolves the ticket's thread and posts into it. A ticket with
// no thread — never escalated, or escalated before this system existed — is
// skipped rather than treated as an error, mirroring the filter step the Zapier
// build used to drop those updates.
func (s *Service) replyInThread(ctx context.Context, ticketID, channelID, threadTS, text string) (*contracts.WebhookResponse, error) {
	t, err := s.resolveThread(ctx, ticketID, channelID, threadTS)
	if err != nil {
		return nil, err
	}

	if t == nil {
		log.Printf("ticket %s has no slack thread, skipping update", ticketID)

		return &contracts.WebhookResponse{
			Message:  "No Slack thread for ticket, update skipped",
			TicketID: ticketID,
			Skipped:  true,
		}, nil
	}

	if _, err := s.slack.PostMessage(ctx, slack.PostMessageRequest{
		Channel:  t.channelID,
		ThreadTS: t.ts,
		Text:     text,
	}); err != nil {
		return nil, err
	}

	return &contracts.WebhookResponse{Message: "Posted to Slack thread", TicketID: ticketID}, nil
}

// resolveThread prefers the join key carried on the webhook and falls back to
// reading the custom fields off the ticket. Returns nil when the ticket has no
// thread.
func (s *Service) resolveThread(ctx context.Context, ticketID, channelID, threadTS string) (*thread, error) {
	if threadTS != "" {
		if channelID == "" {
			channelID = s.slack.DefaultChannel()
		}

		return &thread{channelID: channelID, ts: threadTS}, nil
	}

	ticket, err := s.zendesk.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}

	ts := ticket.CustomFieldValue(s.cfg.ThreadTSFieldID)
	if ts == "" {
		return nil, nil
	}

	channel := ticket.CustomFieldValue(s.cfg.ChannelIDFieldID)
	if channel == "" {
		channel = s.slack.DefaultChannel()
	}

	return &thread{channelID: channel, ts: ts}, nil
}

// buildEscalationMessage renders the parent alert. Free-text fields go in
// inline code so multi-line customer text stays readable in Slack.
func buildEscalationMessage(p contracts.EscalationWebhook) string {
	var b strings.Builder

	b.WriteString("🚨 *Ticket Escalated to Engineering*\n\n")
	fmt.Fprintf(&b, "🎫 *Ticket:* %s\n", ticketLink(p))
	writeLine(&b, "👤 *Customer:*", nameWithEmail(p.RequesterName, p.RequesterMail))
	writeLine(&b, "🏷 *Brand:*", p.Brand)
	writeLine(&b, "🛠 *Escalated by:*", nameWithEmail(p.AgentName, p.AgentMail))
	writeLine(&b, "⚡ *Priority:*", p.Priority)
	writeLine(&b, "📌 *Status:*", p.Status)
	writeLine(&b, "📱 *Device Info:*", joinNonEmpty(" — ", p.DeviceType, p.DeviceModel))
	writeLine(&b, "🆔 *Device IDs:*", p.DeviceIDs)
	writeBlock(&b, "📋 *Reported Issue:*", p.DescribeIssue)
	writeBlock(&b, "📝 *Detailed Description:*", p.Description)
	writeBlock(&b, "📒 *Internal Notes:*", p.Notes)

	return strings.TrimRight(b.String(), "\n")
}

func buildCommentMessage(p contracts.CommentWebhook) string {
	comment := truncate(p.Comment, commentPreviewLimit)

	if p.Author != "" {
		return fmt.Sprintf("💬 *New Comment* from %s:\n```%s```", p.Author, comment)
	}

	return fmt.Sprintf("💬 *New Comment:*\n```%s```", comment)
}

func buildStatusMessage(p contracts.StatusWebhook) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s *Status Updated to:* %s", statusEmoji(p.Status), p.Status)
	if p.Previous != "" {
		fmt.Fprintf(&b, " _(was %s)_", p.Previous)
	}
	if p.Assignee != "" {
		fmt.Fprintf(&b, "\n👤 *Assignee:* %s", p.Assignee)
	}

	return b.String()
}

// ticketLink renders a Slack link when a URL is present, and degrades to a
// plain reference when it isn't.
func ticketLink(p contracts.EscalationWebhook) string {
	label := fmt.Sprintf("#%s", p.TicketID)
	if p.Subject != "" {
		label = fmt.Sprintf("#%s — %s", p.TicketID, p.Subject)
	}

	if p.TicketURL == "" {
		return label
	}

	return fmt.Sprintf("<%s|%s>", p.TicketURL, label)
}

func writeLine(b *strings.Builder, label, value string) {
	if value == "" {
		return
	}

	fmt.Fprintf(b, "%s %s\n", label, value)
}

func writeBlock(b *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}

	fmt.Fprintf(b, "%s\n```%s```\n", label, strings.TrimSpace(value))
}

func nameWithEmail(name, email string) string {
	switch {
	case name == "" && email == "":
		return ""
	case email == "":
		return name
	case name == "":
		return email
	default:
		return fmt.Sprintf("%s (%s)", name, email)
	}
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}

	return strings.Join(kept, sep)
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit]) + "…"
}
