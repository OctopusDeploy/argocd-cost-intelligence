package config

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Server   ServerConfig   `envconfig:"SERVER"`
	Database DatabaseConfig `envconfig:"DB"`
	OpenCost OpenCostConfig `envconfig:"OPENCOST"`
	// WebhookSecret is the bearer token Argo CD Notifications must send to POST /on-sync.
	WebhookSecret string `envconfig:"WEBHOOK_SECRET"`

	// ClusterScopeApp ("<namespace>:<name>") is the Argo CD Application whose viewers may see
	// cluster-wide cost data. Empty turns cluster-wide endpoints off.
	ClusterScopeApp string `envconfig:"CLUSTER_SCOPE_APP"`

	// DailyHistoryDays is how many days of each application's daily cost are kept, so the Trends tab
	// can reach further back than OpenCost's Prometheus. 0 turns recording off.
	DailyHistoryDays int `envconfig:"DAILY_HISTORY_DAYS" default:"400"`

	// VPAEnabled reads VerticalPodAutoscaler recommendations (from Goldilocks or any VPA) for the
	// rightsizing advice. It needs the service to run in the cluster with permission to list them.
	VPAEnabled bool `envconfig:"VPA_ENABLED" default:"true"`

	// Mode is what the process runs: ModeAPI, the API and webhook, or ModeExporter, the singleton that
	// exports each Application's cost to Prometheus.
	Mode string `envconfig:"MODE" default:"api"`

	// MetricsPort is the port /metrics is served on, apart from the API port so a NetworkPolicy can
	// admit Prometheus to it alone. 0 turns metrics off in the API.
	MetricsPort int `envconfig:"METRICS_PORT" default:"9102"`

	// ExporterInterval is how often the exporter measures every Application's cost.
	ExporterInterval time.Duration `envconfig:"EXPORTER_INTERVAL" default:"5m"`
}

// Modes the service runs in.
const (
	ModeAPI      = "api"
	ModeExporter = "exporter"
)

type ServerConfig struct {
	Port         int           `envconfig:"PORT" default:"8080"`
	ReadTimeout  time.Duration `envconfig:"READ_TIMEOUT" default:"10s"`
	WriteTimeout time.Duration `envconfig:"WRITE_TIMEOUT" default:"10s"`
	IdleTimeout  time.Duration `envconfig:"IDLE_TIMEOUT" default:"120s"`
}

type DatabaseConfig struct {
	Host     string `envconfig:"HOST" default:"localhost"`
	Port     int    `envconfig:"PORT" default:"5432"`
	User     string `envconfig:"USER" default:"cost_user"`
	Password string `envconfig:"PASSWORD" required:"true"`
	Database string `envconfig:"NAME" default:"cost_intelligence"`
	SSLMode  string `envconfig:"SSL_MODE" default:"disable"`
	MaxOpen  int    `envconfig:"MAX_OPEN_CONNS" default:"25"`
	MaxIdle  int    `envconfig:"MAX_IDLE_CONNS" default:"5"`
}

type OpenCostConfig struct {
	BaseURL string        `envconfig:"BASE_URL" default:"http://opencost:9003"`
	Timeout time.Duration `envconfig:"TIMEOUT" default:"10s"`
}

func LoadConfig() (*Config, error) {
	var cfg Config

	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to process environment config: %w", err)
	}

	return &cfg, nil
}

func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host,
		c.Port,
		c.User,
		c.Password,
		c.Database,
		c.SSLMode,
	)
}

// minWebhookSecretLength rejects placeholder secrets. The Helm chart generates 32 characters.
const minWebhookSecretLength = 16

func (c *Config) Validate() error {
	if c.Database.Password == "" {
		return errors.New("database password is required")
	}
	if c.MetricsPort < 0 || c.MetricsPort > 65535 {
		return fmt.Errorf("METRICS_PORT %d is not a port; 0 turns metrics off", c.MetricsPort)
	}
	switch c.Mode {
	case ModeExporter:
		if c.MetricsPort == 0 {
			return errors.New("METRICS_PORT is required in exporter mode: the exporter serves nothing else")
		}
		if c.ExporterInterval < time.Minute {
			return fmt.Errorf("EXPORTER_INTERVAL %s is too short: each refresh queries OpenCost for every namespace, so it must be at least 1m", c.ExporterInterval)
		}
		return validateOpenCostURL(c.OpenCost.BaseURL)
	case ModeAPI, "": // the zero value is the API, the default
	default:
		return fmt.Errorf("MODE %q is not %q or %q", c.Mode, ModeAPI, ModeExporter)
	}
	if c.MetricsPort != 0 && c.MetricsPort == c.Server.Port {
		return fmt.Errorf("METRICS_PORT and SERVER_PORT are both %d: metrics need a port of their own", c.MetricsPort)
	}
	if c.DailyHistoryDays < 0 {
		return errors.New("DAILY_HISTORY_DAYS must not be negative; 0 turns daily cost history off")
	}
	if len(c.WebhookSecret) < minWebhookSecretLength {
		return fmt.Errorf("WEBHOOK_SECRET is required and must be at least %d characters: it authenticates Argo CD Notifications to POST /on-sync", minWebhookSecretLength)
	}

	return validateOpenCostURL(c.OpenCost.BaseURL)
}

// validateOpenCostURL ensures the configured OpenCost URL cannot be used for
// SSRF by restricting it to http/https schemes with a non-empty host.
func validateOpenCostURL(rawURL string) error {
	if rawURL == "" {
		return errors.New("opencost base URL is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("opencost base URL is invalid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("opencost base URL scheme must be http or https, got %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("opencost base URL must include a hostname")
	}
	return nil
}
