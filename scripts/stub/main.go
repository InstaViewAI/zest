// Command stub runs fake Slack and Zendesk APIs on :8777 so the escalation
// flows can be exercised end to end without real credentials. It prints every
// request body it receives, which is the point: you see exactly what the
// service would have sent upstream.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const addr = "127.0.0.1:8777"

func main() {
	http.HandleFunc("/slack/", slackHandler)
	http.HandleFunc("/zendesk/", zendeskHandler)

	log.Printf("stub upstreams listening on http://%s", addr)
	log.Printf("  SLACK_BASE_URL=http://%s/slack", addr)
	log.Printf("  ZENDESK_BASE_URL=http://%s/zendesk", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

func slackHandler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	fmt.Printf("\n\033[36m[SLACK %s]\033[0m\n%s\n", strings.TrimPrefix(r.URL.Path, "/slack/"), prettyText(body))

	writeJSON(w, `{"ok":true,"channel":"C123","ts":"1725200000.000100"}`)
}

func zendeskHandler(w http.ResponseWriter, r *http.Request) {
	// Search powers the stale-ticket reports; return two aged tickets.
	if strings.Contains(r.URL.Path, "search.json") {
		old := time.Now().Add(-12 * 24 * time.Hour).Format(time.RFC3339)
		fmt.Printf("\n\033[33m[ZENDESK SEARCH]\033[0m %s\n", r.URL.Query().Get("query"))
		writeJSON(w, fmt.Sprintf(`{"results":[
			{"id":9001,"subject":"Camera offline","status":"open","updated_at":%q},
			{"id":9002,"subject":"App crash","status":"pending","updated_at":%q}]}`, old, old))

		return
	}

	body, _ := io.ReadAll(r.Body)
	fmt.Printf("\n\033[33m[ZENDESK %s %s]\033[0m\n%s\n", r.Method, r.URL.Path, prettyText(body))

	// A GET is the join-key recovery path: return a ticket that carries the
	// custom fields the service looks for.
	writeJSON(w, `{"ticket":{"id":4242,"status":"open","subject":"Camera offline",
		"custom_fields":[{"id":111,"value":"1725200000.000100"},{"id":222,"value":"C123"}]}}`)
}

// prettyText renders a JSON body, unescaping the Slack "text" field so the
// message is readable as it would appear in Slack.
func prettyText(body []byte) string {
	if len(body) == 0 {
		return "  (empty)"
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return string(body)
	}

	if text, ok := parsed["text"].(string); ok {
		delete(parsed, "text")
		meta, _ := json.Marshal(parsed)

		return fmt.Sprintf("  meta: %s\n  ---\n%s", meta, indent(text))
	}

	out, _ := json.MarshalIndent(parsed, "  ", "  ")

	return "  " + string(out)
}

func indent(s string) string {
	return "  | " + strings.ReplaceAll(s, "\n", "\n  | ")
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}
