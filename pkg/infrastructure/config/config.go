package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// AppConfig holds every knob the service needs, loaded once at boot.
type AppConfig struct {
	Env        string
	Server     ServerConfig
	Slack      SlackConfig
	Zendesk    ZendeskConfig
	Escalation EscalationConfig
	Report     ReportConfig
}

type ServerConfig struct {
	Name            string
	Host            string
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type SlackConfig struct {
	BaseURL string
	Token   string
	Timeout time.Duration

	// ChannelID is the escalation channel (a C... id, never a name).
	ChannelID string
	// BotName and IconURL give the posts the "ZEST" bot identity. Both need
	// the chat:write.customize scope on the bot token.
	BotName       string
	ReportBotName string
	IconURL       string
}

type ZendeskConfig struct {
	// BaseURL overrides the URL derived from Subdomain. Normally empty; set it
	// to point the service at a stub in tests or a sandbox.
	BaseURL   string
	Subdomain string
	Email     string
	APIToken  string
	Timeout   time.Duration

	// WebhookSecret is the Zendesk webhook signing secret. When empty,
	// signature verification is skipped — acceptable locally, not in prod.
	WebhookSecret string
}

type EscalationConfig struct {
	// Tag applied to a ticket once its Slack thread exists, and the tag the
	// stale-ticket reports search on.
	Tag string
	// ThreadTSFieldID and ChannelIDFieldID are the numeric ids of the
	// slack_thread_ts and slack_channel_id custom fields. They are the join
	// key between a ticket and its Slack thread.
	ThreadTSFieldID  int64
	ChannelIDFieldID int64
}

type ReportConfig struct {
	// StaleAfter is how long an escalated ticket may go without an update
	// before a report picks it up.
	StaleAfter time.Duration
	Timezone   string

	WeeklyEnabled bool
	WeeklyCron    string

	MonthlyEnabled bool
	MonthlyCron    string
}

// LoadConfig reads configuration from the environment and validates it.
func LoadConfig() (*AppConfig, error) {
	cfg := &AppConfig{
		Env: getEnv("ENVIRONMENT", "local"),
		Server: ServerConfig{
			Name:            getEnv("SERVER_NAME", "zest"),
			Host:            getEnv("SERVER_HOST", ""),
			Port:            getEnvInt("SERVER_PORT", 8080),
			ReadTimeout:     getEnvDuration("SERVER_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:    getEnvDuration("SERVER_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: getEnvDuration("SERVER_SHUTDOWN_TIMEOUT", 5*time.Second),
		},
		Slack: SlackConfig{
			BaseURL:       getEnv("SLACK_BASE_URL", "https://slack.com/api"),
			Token:         getEnv("SLACK_BOT_TOKEN", ""),
			Timeout:       getEnvDuration("SLACK_TIMEOUT", 10*time.Second),
			ChannelID:     getEnv("SLACK_CHANNEL_ID", ""),
			BotName:       getEnv("SLACK_BOT_NAME", "ZEST"),
			ReportBotName: getEnv("SLACK_REPORT_BOT_NAME", "ZEST - Report"),
			IconURL:       getEnv("SLACK_ICON_URL", ""),
		},
		Zendesk: ZendeskConfig{
			BaseURL:       getEnv("ZENDESK_BASE_URL", ""),
			Subdomain:     getEnv("ZENDESK_SUBDOMAIN", ""),
			Email:         getEnv("ZENDESK_EMAIL", ""),
			APIToken:      getEnv("ZENDESK_API_TOKEN", ""),
			Timeout:       getEnvDuration("ZENDESK_TIMEOUT", 10*time.Second),
			WebhookSecret: getEnv("ZENDESK_WEBHOOK_SECRET", ""),
		},
		Escalation: EscalationConfig{
			Tag:              getEnv("ESCALATION_TAG", "escalated"),
			ThreadTSFieldID:  getEnvInt64("ZENDESK_FIELD_SLACK_THREAD_TS", 0),
			ChannelIDFieldID: getEnvInt64("ZENDESK_FIELD_SLACK_CHANNEL_ID", 0),
		},
		Report: ReportConfig{
			StaleAfter:     getEnvDuration("REPORT_STALE_AFTER", 168*time.Hour),
			Timezone:       getEnv("REPORT_TIMEZONE", "Local"),
			WeeklyEnabled:  getEnvBool("REPORT_WEEKLY_ENABLED", true),
			WeeklyCron:     getEnv("REPORT_WEEKLY_CRON", "30 1 * * 6"),
			MonthlyEnabled: getEnvBool("REPORT_MONTHLY_ENABLED", false),
			MonthlyCron:    getEnv("REPORT_MONTHLY_CRON", "30 1 1 * *"),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validate fails fast on missing configuration so we don't discover it on the
// first request instead of at boot.
func (c *AppConfig) validate() error {
	missing := []string{}

	if c.Slack.Token == "" {
		missing = append(missing, "SLACK_BOT_TOKEN")
	}
	if c.Slack.ChannelID == "" {
		missing = append(missing, "SLACK_CHANNEL_ID")
	}
	if c.Zendesk.Subdomain == "" {
		missing = append(missing, "ZENDESK_SUBDOMAIN")
	}
	if c.Zendesk.Email == "" {
		missing = append(missing, "ZENDESK_EMAIL")
	}
	if c.Zendesk.APIToken == "" {
		missing = append(missing, "ZENDESK_API_TOKEN")
	}
	if c.Escalation.ThreadTSFieldID == 0 {
		missing = append(missing, "ZENDESK_FIELD_SLACK_THREAD_TS")
	}
	if c.Escalation.ChannelIDFieldID == 0 {
		missing = append(missing, "ZENDESK_FIELD_SLACK_CHANNEL_ID")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %v", missing)
	}

	// Slack rejects name-based channel references in several APIs, so catch the
	// mistake at boot rather than on the first escalation.
	if strings.HasPrefix(c.Slack.ChannelID, "#") {
		return fmt.Errorf("SLACK_CHANNEL_ID must be a channel id (C...), not a name: %q", c.Slack.ChannelID)
	}

	if _, err := time.LoadLocation(c.Report.Timezone); err != nil {
		return fmt.Errorf("invalid REPORT_TIMEZONE %q: %w", c.Report.Timezone, err)
	}

	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}

	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if v, err := strconv.ParseInt(os.Getenv(key), 10, 64); err == nil {
		return v
	}

	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(key)); err == nil {
		return v
	}

	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return v
	}

	return fallback
}
