package escalation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"zest/pkg/application/slack"
	"zest/pkg/application/zendesk"
	"zest/pkg/contracts"
	"zest/pkg/infrastructure/config"
)

const (
	threadTSFieldID  = int64(111)
	channelIDFieldID = int64(222)
)

// capture records the request bodies each stub upstream received.
type capture struct {
	slackBodies   []map[string]any
	zendeskBodies []map[string]any
	zendeskPaths  []string
}

// newTestService stands up stub Slack and Zendesk servers and wires a Service
// against them. ticketJSON, when non-empty, is what GET /tickets/{id}.json
// returns.
func newTestService(t *testing.T, ticketJSON string) (*Service, *capture) {
	t.Helper()

	cap := &capture{}

	slackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		cap.slackBodies = append(cap.slackBodies, parsed)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C123","ts":"1725200000.000100"}`))
	}))
	t.Cleanup(slackSrv.Close)

	zendeskSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.zendeskPaths = append(cap.zendeskPaths, r.Method+" "+r.URL.Path)

		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(ticketJSON))
			return
		}

		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		cap.zendeskBodies = append(cap.zendeskBodies, parsed)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ticket":{"id":1,"status":"open"}}`))
	}))
	t.Cleanup(zendeskSrv.Close)

	slackSvc := slack.NewService(config.SlackConfig{
		BaseURL:   slackSrv.URL,
		Token:     "test-token",
		ChannelID: "C123",
		BotName:   "ZEST",
	})

	zendeskSvc := zendesk.NewService(config.ZendeskConfig{
		BaseURL:  zendeskSrv.URL,
		Email:    "a@b.c",
		APIToken: "t",
	})

	svc := NewService(slackSvc, zendeskSvc, config.EscalationConfig{
		Tag:              "escalated",
		ThreadTSFieldID:  threadTSFieldID,
		ChannelIDFieldID: channelIDFieldID,
	})

	return svc, cap
}

func TestHandleEscalationWritesJoinKeyBackToTicket(t *testing.T) {
	svc, cap := newTestService(t, "")

	res, err := svc.HandleEscalation(context.Background(), contracts.EscalationWebhook{
		TicketID:      "4242",
		Subject:       "Camera offline",
		TicketURL:     "https://test.zendesk.com/agent/tickets/4242",
		RequesterName: "Jamie Lee",
		RequesterMail: "jamie@example.com",
		Priority:      "urgent",
		Status:        "open",
		DescribeIssue: "Feed drops every few minutes",
	})
	if err != nil {
		t.Fatalf("HandleEscalation: %v", err)
	}

	if res.ThreadTS != "1725200000.000100" || res.ChannelID != "C123" {
		t.Fatalf("unexpected join key: %+v", res)
	}

	if len(cap.zendeskBodies) != 1 {
		t.Fatalf("expected 1 zendesk update, got %d", len(cap.zendeskBodies))
	}

	ticket, ok := cap.zendeskBodies[0]["ticket"].(map[string]any)
	if !ok {
		t.Fatalf("update body missing ticket envelope: %v", cap.zendeskBodies[0])
	}

	tags, _ := ticket["additional_tags"].([]any)
	if len(tags) != 1 || tags[0] != "escalated" {
		t.Errorf("expected escalated tag to be appended, got %v", ticket["additional_tags"])
	}

	fields, _ := ticket["custom_fields"].([]any)
	if len(fields) != 2 {
		t.Fatalf("expected 2 custom fields, got %v", ticket["custom_fields"])
	}

	got := map[float64]string{}
	for _, f := range fields {
		m := f.(map[string]any)
		got[m["id"].(float64)] = m["value"].(string)
	}

	if got[float64(threadTSFieldID)] != "1725200000.000100" {
		t.Errorf("thread ts field not written: %v", got)
	}
	if got[float64(channelIDFieldID)] != "C123" {
		t.Errorf("channel id field not written: %v", got)
	}
}

func TestHandleCommentUsesJoinKeyFromPayload(t *testing.T) {
	svc, cap := newTestService(t, "")

	res, err := svc.HandleComment(context.Background(), contracts.CommentWebhook{
		TicketID:  "4242",
		Comment:   "Customer says it is still failing",
		Author:    "Jamie Lee",
		ChannelID: "C999",
		ThreadTS:  "1725200000.000100",
	})
	if err != nil {
		t.Fatalf("HandleComment: %v", err)
	}
	if res.Skipped {
		t.Fatal("expected comment to be posted, got skipped")
	}

	// The payload carried the join key, so the ticket should not be fetched.
	if len(cap.zendeskPaths) != 0 {
		t.Errorf("expected no zendesk calls, got %v", cap.zendeskPaths)
	}

	if len(cap.slackBodies) != 1 {
		t.Fatalf("expected 1 slack post, got %d", len(cap.slackBodies))
	}
	if cap.slackBodies[0]["thread_ts"] != "1725200000.000100" {
		t.Errorf("comment not threaded: %v", cap.slackBodies[0])
	}
	if cap.slackBodies[0]["channel"] != "C999" {
		t.Errorf("expected payload channel to win: %v", cap.slackBodies[0])
	}
}

func TestHandleCommentFallsBackToTicketCustomFields(t *testing.T) {
	ticketJSON := `{"ticket":{"id":4242,"status":"open","custom_fields":[
		{"id":111,"value":"1725200000.000100"},
		{"id":222,"value":"C777"}]}}`

	svc, cap := newTestService(t, ticketJSON)

	if _, err := svc.HandleComment(context.Background(), contracts.CommentWebhook{
		TicketID: "4242",
		Comment:  "no join key on this payload",
	}); err != nil {
		t.Fatalf("HandleComment: %v", err)
	}

	if len(cap.zendeskPaths) != 1 || !strings.HasPrefix(cap.zendeskPaths[0], "GET ") {
		t.Fatalf("expected one ticket fetch, got %v", cap.zendeskPaths)
	}
	if cap.slackBodies[0]["thread_ts"] != "1725200000.000100" {
		t.Errorf("thread ts not recovered from ticket: %v", cap.slackBodies[0])
	}
	if cap.slackBodies[0]["channel"] != "C777" {
		t.Errorf("channel not recovered from ticket: %v", cap.slackBodies[0])
	}
}

func TestHandleCommentSkipsTicketWithNoThread(t *testing.T) {
	// A ticket that was never escalated has no join-key custom fields.
	svc, cap := newTestService(t, `{"ticket":{"id":4242,"status":"open","custom_fields":[]}}`)

	res, err := svc.HandleComment(context.Background(), contracts.CommentWebhook{
		TicketID: "4242",
		Comment:  "comment on a non-escalated ticket",
	})
	if err != nil {
		t.Fatalf("expected skip, got error: %v", err)
	}
	if !res.Skipped {
		t.Error("expected Skipped=true for ticket with no thread")
	}
	if len(cap.slackBodies) != 0 {
		t.Errorf("expected no slack post, got %v", cap.slackBodies)
	}
}

func TestHandleStatusChangePostsThreadedReply(t *testing.T) {
	svc, cap := newTestService(t, "")

	if _, err := svc.HandleStatusChange(context.Background(), contracts.StatusWebhook{
		TicketID: "4242",
		Status:   "solved",
		Previous: "pending",
		ThreadTS: "1725200000.000100",
	}); err != nil {
		t.Fatalf("HandleStatusChange: %v", err)
	}

	text, _ := cap.slackBodies[0]["text"].(string)
	if !strings.Contains(text, "Status Updated to:") || !strings.Contains(text, "solved") {
		t.Errorf("unexpected status message: %q", text)
	}
	if !strings.Contains(text, "was pending") {
		t.Errorf("previous status missing: %q", text)
	}
}

func TestBuildEscalationMessageOmitsEmptyFields(t *testing.T) {
	msg := buildEscalationMessage(contracts.EscalationWebhook{
		TicketID:  "77",
		Subject:   "Door sensor",
		TicketURL: "https://x.zendesk.com/agent/tickets/77",
		Priority:  "high",
	})

	if !strings.Contains(msg, "<https://x.zendesk.com/agent/tickets/77|#77 — Door sensor>") {
		t.Errorf("ticket link missing: %q", msg)
	}
	if !strings.Contains(msg, "⚡ *Priority:* high") {
		t.Errorf("priority missing: %q", msg)
	}
	// Brand, device info and the free-text blocks were all empty.
	for _, absent := range []string{"Brand:", "Device Info:", "Reported Issue:", "Internal Notes:"} {
		if strings.Contains(msg, absent) {
			t.Errorf("expected %q to be omitted from %q", absent, msg)
		}
	}
}

func TestBuildCommentMessageTruncatesLongComments(t *testing.T) {
	long := strings.Repeat("x", commentPreviewLimit+50)

	msg := buildCommentMessage(contracts.CommentWebhook{Comment: long})
	if !strings.Contains(msg, "…") {
		t.Error("expected long comment to be truncated")
	}
	if strings.Count(msg, "x") != commentPreviewLimit {
		t.Errorf("expected %d chars kept, got %d", commentPreviewLimit, strings.Count(msg, "x"))
	}
}
