package escalation

import (
	"strings"
	"testing"
	"time"

	"zest/pkg/application/zendesk"
)

func TestBuildReportMessageGroupsByStatusOldestFirst(t *testing.T) {
	now := time.Now()
	tickets := []zendesk.Ticket{
		{ID: 1, Subject: "Newer open", Status: "open", UpdatedAt: now.Add(-8 * 24 * time.Hour)},
		{ID: 2, Subject: "Older open", Status: "open", UpdatedAt: now.Add(-20 * 24 * time.Hour)},
		{ID: 3, Subject: "Pending one", Status: "pending", UpdatedAt: now.Add(-9 * 24 * time.Hour)},
		{ID: 4, Subject: "Odd status", Status: "escalated_to_vendor", UpdatedAt: now.Add(-10 * 24 * time.Hour)},
	}

	msg := buildReportMessage("Weekly Escalation Report", tickets, 168*time.Hour, "acme")

	if !strings.Contains(msg, "*Total:* 4") {
		t.Errorf("total missing: %q", msg)
	}
	if !strings.Contains(msg, "*OPEN* (2)") || !strings.Contains(msg, "*PENDING* (1)") {
		t.Errorf("status grouping wrong: %q", msg)
	}
	// An unrecognised Zendesk status must not be dropped from the digest.
	if !strings.Contains(msg, "*UNKNOWN* (1)") {
		t.Errorf("unknown status bucket missing: %q", msg)
	}

	older := strings.Index(msg, "Older open")
	newer := strings.Index(msg, "Newer open")
	if older == -1 || newer == -1 || older > newer {
		t.Errorf("expected oldest ticket first within a group: %q", msg)
	}

	if !strings.Contains(msg, "<https://acme.zendesk.com/agent/tickets/1|#1 — Newer open>") {
		t.Errorf("agent link missing: %q", msg)
	}
}

func TestBuildReportMessageAllClearWhenEmpty(t *testing.T) {
	msg := buildReportMessage("Weekly Escalation Report", nil, 168*time.Hour, "acme")

	if !strings.Contains(msg, "Nothing stale") {
		t.Errorf("expected all-clear message, got %q", msg)
	}
	if !strings.Contains(msg, "no update in 7d") {
		t.Errorf("expected window in header, got %q", msg)
	}
}

func TestStatusEmojiFallsBackToUnknown(t *testing.T) {
	if statusEmoji("open") == statusEmoji("something-else") {
		t.Error("expected known and unknown statuses to differ")
	}
	if statusEmoji("SOLVED") != statusEmojis["solved"] {
		t.Error("expected status lookup to be case-insensitive")
	}
}
