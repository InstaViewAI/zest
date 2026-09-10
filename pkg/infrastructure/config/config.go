package config

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// AppConfig holds every knob the service needs, loaded once at boot from a
// config file and a secret file (see LoadConfig).
type AppConfig struct {
	configReader *viper.Viper
	secretReader *viper.Viper
	Env          string           `mapstructure:"env"`
	Server       ServerConfig     `mapstructure:"server"`
	Slack        SlackConfig      `mapstructure:"slack"`
	Zendesk      ZendeskConfig    `mapstructure:"zendesk"`
	Escalation   EscalationConfig `mapstructure:"escalation"`
	Report       ReportConfig     `mapstructure:"report"`
}

type ServerConfig struct {
	Name            string        `mapstructure:"name" validate:"required"`
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port" validate:"required"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

type SlackConfig struct {
	BaseURL string        `mapstructure:"base_url" validate:"required"`
	Token   string        `mapstructure:"token" validate:"required"`
	Timeout time.Duration `mapstructure:"timeout"`

	// ChannelID is the escalation channel (a C... id, never a name).
	ChannelID string `mapstructure:"channel_id" validate:"required"`
	// BotName and IconURL give the posts the "ZEST" bot identity. Both need
	// the chat:write.customize scope on the bot token.
	BotName       string `mapstructure:"bot_name"`
	ReportBotName string `mapstructure:"report_bot_name"`
	IconURL       string `mapstructure:"icon_url"`
}

type ZendeskConfig struct {
	// BaseURL overrides the URL derived from Subdomain. Normally empty; set it
	// to point the service at a stub in tests or a sandbox.
	BaseURL   string        `mapstructure:"base_url"`
	Subdomain string        `mapstructure:"subdomain" validate:"required"`
	Email     string        `mapstructure:"email" validate:"required"`
	APIToken  string        `mapstructure:"api_token" validate:"required"`
	Timeout   time.Duration `mapstructure:"timeout"`

	// WebhookSecret is the Zendesk webhook signing secret. When empty,
	// signature verification is skipped — acceptable locally, not in prod.
	WebhookSecret string `mapstructure:"webhook_secret"`
}

type EscalationConfig struct {
	// Tag applied to a ticket once its Slack thread exists, and the tag the
	// stale-ticket reports search on.
	Tag string `mapstructure:"tag" validate:"required"`
	// ThreadTSFieldID and ChannelIDFieldID are the numeric ids of the
	// slack_thread_ts and slack_channel_id custom fields. They are the join
	// key between a ticket and its Slack thread.
	ThreadTSFieldID  int64 `mapstructure:"thread_ts_field_id" validate:"required"`
	ChannelIDFieldID int64 `mapstructure:"channel_id_field_id" validate:"required"`
}

type ReportConfig struct {
	// StaleAfter is how long an escalated ticket may go without an update
	// before a report picks it up.
	StaleAfter time.Duration `mapstructure:"stale_after" validate:"required"`
	Timezone   string        `mapstructure:"timezone" validate:"required,timezone"`

	WeeklyEnabled bool   `mapstructure:"weekly_enabled"`
	WeeklyCron    string `mapstructure:"weekly_cron" validate:"required_if=WeeklyEnabled true"`

	MonthlyEnabled bool   `mapstructure:"monthly_enabled"`
	MonthlyCron    string `mapstructure:"monthly_cron" validate:"required_if=MonthlyEnabled true"`
}

// LoadDefaultConfig only sets default values. Anything provided in the config
// or secret file overrides them.
func LoadDefaultConfig() *AppConfig {
	return &AppConfig{
		configReader: viper.New(),
		secretReader: viper.New(),
		Env:          "local",
		Server: ServerConfig{
			Name:            "zest",
			Port:            8080,
			ReadTimeout:     10 * time.Second,
			WriteTimeout:    30 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		Slack: SlackConfig{
			BaseURL:       "https://slack.com/api",
			Timeout:       10 * time.Second,
			BotName:       "ZEST",
			ReportBotName: "ZEST - Report",
		},
		Zendesk: ZendeskConfig{
			Timeout: 10 * time.Second,
		},
		Escalation: EscalationConfig{
			Tag: "escalated",
		},
		Report: ReportConfig{
			StaleAfter:     168 * time.Hour,
			Timezone:       "Local",
			WeeklyEnabled:  true,
			WeeklyCron:     "30 1 * * 6",
			MonthlyEnabled: false,
			MonthlyCron:    "30 1 1 * *",
		},
	}
}

// LoadConfig loads configuration from yaml files.
// Steps:-
// 1. Read the config file into viper's config reader
// 2. Read the secret file (if any) into viper's secret reader
// 3. Merge the secret reader into the config reader, secrets taking precedence
// 4. Unmarshal into the go struct, overriding the defaults
// 5. Validate, so a missing value surfaces at boot rather than mid-escalation
// 6. Set the environment from ENVIRONMENT directly
func LoadConfig(configPath, secretPath, env string) (*AppConfig, error) {
	cfg := LoadDefaultConfig()

	cfg.configReader.SetConfigFile(configPath)
	if err := cfg.configReader.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file. err: %w", err)
	}

	if secretPath != "" {
		cfg.secretReader.SetConfigFile(secretPath)
		if err := cfg.secretReader.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read secrets file. err: %w", err)
		}

		if err := cfg.configReader.MergeConfigMap(cfg.secretReader.AllSettings()); err != nil {
			return nil, fmt.Errorf("failed to merge secrets configs. err: %w", err)
		}
	}

	if err := cfg.configReader.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config file. err: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate configs struct. err: %w", err)
	}

	if env != "" {
		cfg.Env = env
	}

	return cfg, nil
}

// Validate validates the AppConfig. Errors name the yaml key (slack.channel_id)
// rather than the go field, so they point straight at the line to fix.
func (appCfg *AppConfig) Validate() error {
	validate := validator.New(validator.WithRequiredStructEnabled())
	validate.RegisterTagNameFunc(func(f reflect.StructField) string {
		return f.Tag.Get("mapstructure")
	})

	if err := validate.Struct(appCfg); err != nil {
		return err
	}

	// Slack rejects name-based channel references in several APIs, so catch the
	// mistake at boot rather than on the first escalation.
	if strings.HasPrefix(appCfg.Slack.ChannelID, "#") {
		return fmt.Errorf("slack.channel_id must be a channel id (C...), not a name: %q", appCfg.Slack.ChannelID)
	}

	return nil
}
