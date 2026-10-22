package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
)

type Config struct {
	Server   ServerConfig   `envconfig:"SERVER"`
	Database DatabaseConfig `envconfig:"DB"`
	OpenCost OpenCostConfig `envconfig:"OPENCOST"`
	// WebhookSecret is the bearer token Argo CD Notifications must send to POST /on-sync.
	WebhookSecret string `envconfig:"WEBHOOK_SECRET"`

	// ClusterScopeApp ("<namespace>:<name>") is the Argo CD Application whose viewers may see
	// cluster-wide cost data. Empty finds one: the Application that installed the service, else
	// ClusterAccessApp. ClusterScopeOff turns cluster-wide endpoints off.
	ClusterScopeApp string `envconfig:"CLUSTER_SCOPE_APP"`

	// ClusterAccessApp ("<namespace>:<name>") is the access Application the Helm chart creates when
	// Helm installs the service, used when no Argo CD Application installed it.
	ClusterAccessApp string `envconfig:"CLUSTER_ACCESS_APP"`

	// ArgoCDNamespace is where Argo CD runs: an Application named without a namespace in Argo CD's
	// tracking annotation is there.
	ArgoCDNamespace string `envconfig:"ARGOCD_NAMESPACE" default:"argocd"`

	// PodNamespace and DeploymentName locate the service's own Deployment, whose tracking annotation
	// names the Application that installed it.
	PodNamespace   string `envconfig:"POD_NAMESPACE"`
	DeploymentName string `envconfig:"DEPLOYMENT_NAME"`

	// DailyHistoryDays is how many days of each application's daily cost are kept, so the Trends tab
	// can reach further back than OpenCost's Prometheus. 0 turns recording off.
	DailyHistoryDays int `envconfig:"DAILY_HISTORY_DAYS" default:"400"`

	// BackfillConcurrency is how many namespaces the startup backfill measures with OpenCost at once.
	// The backfill measures each Argo CD Application with no cost record yet when the service starts.
	// 0 turns it off.
	BackfillConcurrency int `envconfig:"BACKFILL_CONCURRENCY" default:"4"`

	// PruneRemoteCost deletes the cost records and daily history stored for Argo CD Applications that
	// deploy to another cluster, at startup and on each daily history run. It can't be undone.
	PruneRemoteCost bool `envconfig:"PRUNE_REMOTE_COST" default:"false"`

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

	// LogLevel is LogLevelInfo or LogLevelDebug. Debug also logs each OpenCost request URL and
	// response body, which name every namespace and cost in the cluster.
	LogLevel string `envconfig:"LOG_LEVEL" default:"info"`
}

// Log levels.
const (
	LogLevelInfo  = "info"
	LogLevelDebug = "debug"
)

// Debug reports whether LOG_LEVEL asks for debug logging.
func (c *Config) Debug() bool {
	return c.LogLevel == LogLevelDebug
}

// ClusterScopeOff, as CLUSTER_SCOPE_APP, turns cluster-wide endpoints off.
const ClusterScopeOff = "off"

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
	// "DEBUG" or "Debug " mean debug too, rather than stopping the service.
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))

	return &cfg, nil
}

func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=5",
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
	switch c.LogLevel {
	case LogLevelInfo, LogLevelDebug, "": // the zero value is info, the default
	default:
		return fmt.Errorf("LOG_LEVEL %q is not %q or %q", c.LogLevel, LogLevelInfo, LogLevelDebug)
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
	if c.BackfillConcurrency < 0 {
		return errors.New("BACKFILL_CONCURRENCY must not be negative; 0 turns the startup backfill off")
	}
	if c.ClusterScopeApp != "" && c.ClusterScopeApp != ClusterScopeOff {
		if _, err := models.ParseAppRef(c.ClusterScopeApp); err != nil {
			return fmt.Errorf("CLUSTER_SCOPE_APP %q: %w, or %q to turn cluster-wide cost data off", c.ClusterScopeApp, err, ClusterScopeOff)
		}
	}
	if c.ClusterAccessApp != "" {
		if _, err := models.ParseAppRef(c.ClusterAccessApp); err != nil {
			return fmt.Errorf("CLUSTER_ACCESS_APP %q: %w", c.ClusterAccessApp, err)
		}
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
