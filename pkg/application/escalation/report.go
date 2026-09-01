package escalation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"zest/pkg/application/slack"
	"zest/pkg/application/zendesk"
	"zest/pkg/contracts"
)

// statusOrder fixes the section order of a digest so successive reports read
// the same way. Anything Zendesk returns outside this list lands in "unknown".
var statusOrder = []string{"new", "open", "pending", "hold", "solved", "closed", "unknown"}

var statusEmojis = map[string]string{
	"new":     "🆕",
	"open":    "📂",
	"pending": "⏳",
	"hold":    "🔒",
	"solved":  "✅",
	"closed":  "📦",
	"unknown": "❔",
}

func statusEmoji(status string) string {
	if e, ok := statusEmojis[strings.ToLower(status)]; ok {
		return e
	}

	return statusEmojis["unknown"]
}

// PostStaleReport finds escalated tickets untouched for longer than staleAfter,
// groups them by status and posts a digest. When nothing is stale it posts an
// all-clear rather than staying silent, so a broken schedule is distinguishable
// from a quiet week.
func (s *Service) PostStaleReport(ctx context.Context, title string, staleAfter time.Duration, subdomain string) (*contracts.ReportResponse, error) {
	cutoff := time.Now().UTC().Add(-staleAfter)
	query := fmt.Sprintf("type:ticket tags:%s updated<%s", s.cfg.Tag, cutoff.Format("2006-01-02T15:04:05Z"))

	tickets, err := s.zendesk.SearchTickets(ctx, query)
	if err != nil {
		return nil, err
	}

	text := buildReportMessage(title, tickets, staleAfter, subdomain)

	if _, err := s.slack.PostMessage(ctx, slack.PostMessageRequest{
		Text:     text,
		Username: s.slack.ReportBotName(),
	}); err != nil {
		return nil, err
	}

	return &contracts.ReportResponse{
		Message: "Report posted",
		Tickets: len(tickets),
		Posted:  true,
	}, nil
}

// buildReportMessage groups tickets by status and renders the digest.
func buildReportMessage(title string, tickets []zendesk.Ticket, staleAfter time.Duration, subdomain string) string {
	var b strings.Builder

	window := formatDuration(staleAfter)

	fmt.Fprintf(&b, "📊 *%s*\n", title)
	fmt.Fprintf(&b, "_Escalated tickets with no update in %s_\n\n", window)

	if len(tickets) == 0 {
		b.WriteString("✅ Nothing stale — every escalated ticket has been touched in the window.")

		return b.String()
	}

	fmt.Fprintf(&b, "*Total:* %d\n", len(tickets))

	grouped := groupByStatus(tickets)
	for _, status := range statusOrder {
		group := grouped[status]
		if len(group) == 0 {
			continue
		}

		// Oldest first — the ticket that has been ignored longest leads.
		sort.Slice(group, func(i, j int) bool {
			return group[i].UpdatedAt.Before(group[j].UpdatedAt)
		})

		fmt.Fprintf(&b, "\n%s *%s* (%d)\n", statusEmoji(status), strings.ToUpper(status), len(group))
		for _, t := range group {
			fmt.Fprintf(&b, "  • %s · _%s_\n", ticketRef(t, subdomain), sinceLabel(t.UpdatedAt))
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func groupByStatus(tickets []zendesk.Ticket) map[string][]zendesk.Ticket {
	grouped := make(map[string][]zendesk.Ticket, len(statusOrder))

	for _, t := range tickets {
		status := strings.ToLower(t.Status)
		if _, known := statusEmojis[status]; !known {
			status = "unknown"
		}

		grouped[status] = append(grouped[status], t)
	}

	return grouped
}

func ticketRef(t zendesk.Ticket, subdomain string) string {
	label := fmt.Sprintf("#%d — %s", t.ID, t.Subject)
	if t.Subject == "" {
		label = fmt.Sprintf("#%d", t.ID)
	}

	if subdomain == "" {
		return label
	}

	return fmt.Sprintf("<https://%s.zendesk.com/agent/tickets/%d|%s>", subdomain, t.ID, label)
}

// sinceLabel renders how long ago a ticket was last touched, in whole days when
// it has been more than a day.
func sinceLabel(updated time.Time) string {
	if updated.IsZero() {
		return "never updated"
	}

	return formatDuration(time.Since(updated)) + " ago"
}

func formatDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}
