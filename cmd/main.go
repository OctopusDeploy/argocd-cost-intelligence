package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/backfill"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/config"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/exporter"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/history"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/hook"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/models"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/opencost"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/rightsizing"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/server"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/storage"
	"github.com/OctopusDeploy/argocd-cost-intelligence/pkg/telemetry"
)

func main() {
	logger := log.New(os.Stdout, "[argocd-cost-intel] ", log.LstdFlags|log.Lshortfile)

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatalf("Failed to load configuration: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		logger.Fatalf("Invalid configuration: %v", err)
	}

	if cfg.Mode == config.ModeExporter {
		runExporter(cfg, logger)
		return
	}
	runAPI(cfg, logger)
}

// runAPI serves the REST API and the sync webhook, records daily cost history, and serves the
// process's own metrics on the metrics port.
func runAPI(cfg *config.Config, logger *log.Logger) {
	registry := prometheus.NewRegistry()
	var metrics *telemetry.Metrics
	if cfg.MetricsPort != 0 {
		metrics = telemetry.NewMetrics(registry)
	}

	serverOpts := []server.Option{server.WithWebhookSecret(cfg.WebhookSecret), server.WithMetrics(metrics)}
	if cfg.ClusterScopeApp != "" {
		app, err := models.ParseAppRef(cfg.ClusterScopeApp)
		if err != nil {
			logger.Fatalf("Invalid CLUSTER_SCOPE_APP %q: %v", cfg.ClusterScopeApp, err)
		}
		serverOpts = append(serverOpts, server.WithClusterScopeApp(app))
		logger.Printf("Cluster-wide cost data gated by application %s\n", app)
	} else {
		logger.Print("Cluster-wide cost data disabled (CLUSTER_SCOPE_APP not set)")
	}

	logger.Print("Starting ArgoCD Cost Intelligence Service")
	logger.Printf("Server listening on port %d\n", cfg.Server.Port)
	logger.Printf("OpenCost endpoint: %s\n", cfg.OpenCost.BaseURL)
	logger.Printf("Database: %s@%s:%d/%s\n", cfg.Database.User, cfg.Database.Host, cfg.Database.Port, cfg.Database.Database)

	// Initialize PostgreSQL storage
	postgres, err := storage.NewPostgresCostStorage(cfg.Database.DSN(), storage.WithPool(cfg.Database.MaxOpen, cfg.Database.MaxIdle))
	if err != nil {
		logger.Fatalf("Failed to initialize PostgreSQL storage: %v", err)
	}
	defer postgres.Close()
	var costStorage storage.CostStorage = postgres
	if metrics != nil {
		costStorage = storage.Instrumented(postgres, metrics.ObserveDatabase)
	}

	// Initialize database schema
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := costStorage.InitializeSchema(ctx); err != nil {
		logger.Fatalf("Failed to initialize database schema: %v", err)
	}
	cancel()

	opencostClient := newOpenCostClient(cfg, metrics, logger)

	// Initialize sync hook
	syncHook := hook.NewSyncHook(
		opencostClient,
		costStorage,
		5*time.Second, // Delay before measuring to allow metrics to propagate
		logger,
	)

	// Read VerticalPodAutoscaler recommendations for rightsizing, when running in a cluster.
	if cfg.VPAEnabled {
		if lister, err := rightsizing.NewInClusterLister(); err != nil {
			logger.Printf("VPA rightsizing off: not running in a Kubernetes cluster (%v)\n", err)
		} else {
			serverOpts = append(serverOpts, server.WithVPALister(rightsizing.NewCachedLister(lister, time.Minute)))
			logger.Print("VPA rightsizing on")
		}
	} else {
		logger.Print("VPA rightsizing off (VPA_ENABLED=false)")
	}

	// Initialize API server
	apiServer := server.NewCostAPIServer(
		syncHook,
		costStorage,
		opencostClient,
		logger,
		serverOpts...,
	)

	// Record each application's daily cost, so long ranges do not depend on Prometheus retention.
	background, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	var recorder *history.Recorder
	if cfg.DailyHistoryDays > 0 {
		recorder = history.NewRecorder(costStorage, opencostClient, cfg.DailyHistoryDays, logger)
		if metrics != nil {
			recorder.OnRun(metrics.ObserveHistoryRun)
		}
		go recorder.Run(background)
		logger.Printf("Recording daily cost history, keeping %d days\n", cfg.DailyHistoryDays)
	} else {
		logger.Print("Daily cost history disabled (DAILY_HISTORY_DAYS=0)")
	}

	// Measure the applications with no cost record yet, so a fresh install shows costs before each
	// application syncs again.
	if cfg.BackfillConcurrency > 0 {
		go runBackfill(background, cfg.BackfillConcurrency, costStorage, opencostClient, recorder, logger)
	} else {
		logger.Print("Startup backfill disabled (BACKFILL_CONCURRENCY=0)")
	}

	servers := []*http.Server{{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      apiServer,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}}
	if metrics != nil {
		mux := http.NewServeMux()
		mux.Handle("/metrics", telemetry.Handler(registry))
		servers = append(servers, metricsServer(cfg.MetricsPort, mux))
		logger.Printf("Metrics on port %d\n", cfg.MetricsPort)
	} else {
		logger.Print("Metrics disabled (METRICS_PORT=0)")
	}

	serveUntilSignal(logger, servers, stopBackground)
}

// runBackfill measures every Argo CD Application without a cost record, after any other replica's run,
// then has the recorder record their history. It runs again a few times while namespaces are left
// unmeasured. Outside a cluster there are no Applications to list.
func runBackfill(ctx context.Context, concurrency int, store storage.CostStorage, client *opencost.Client, recorder *history.Recorder, logger *log.Logger) {
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Printf("Startup backfill off: not running in a Kubernetes cluster (%v)\n", err)
		return
	}
	kubeConfig.Timeout = 30 * time.Second
	kubeClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		logger.Printf("Startup backfill off: %v\n", err)
		return
	}

	b := backfill.New(exporter.NewKubeApplicationLister(kubeClient), store, client, concurrency, logger)
	b.RunWithRetries(ctx, backfill.RetryDelays, func(result backfill.Result, err error) {
		if err != nil {
			logger.Printf("Startup backfill: %v\n", err)
			return
		}
		logger.Printf("Startup backfill: %d measured, %d already recorded, %d on other clusters, %d without a destination namespace, "+
			"%d in namespaces OpenCost has no data for yet, %d failed\n",
			result.Saved, result.Recorded, result.Remote, result.NoNamespace, result.NoData, result.Failed)
		if result.Saved > 0 && recorder != nil {
			recorder.RecordSoon()
		}
	})
}

// runExporter exports every Argo CD Application's cost to Prometheus. It runs as a single replica:
// see pkg/exporter.
func runExporter(cfg *config.Config, logger *log.Logger) {
	logger.Printf("Starting ArgoCD Cost Intelligence exporter, refreshing every %s\n", cfg.ExporterInterval)
	registry := prometheus.NewRegistry()
	metrics := telemetry.NewExporterMetrics(registry)

	postgres, err := storage.NewPostgresCostStorage(cfg.Database.DSN(), storage.WithPool(cfg.Database.MaxOpen, cfg.Database.MaxIdle))
	if err != nil {
		logger.Fatalf("Failed to initialize PostgreSQL storage: %v", err)
	}
	defer postgres.Close()
	// The API owns the schema; the exporter only reads cloud estimates.
	costStorage := storage.Instrumented(postgres, metrics.ObserveDatabase)

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatalf("The exporter lists Argo CD Applications, so it must run in the cluster: %v", err)
	}
	kubeClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		logger.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	exp := exporter.New(
		exporter.NewKubeApplicationLister(kubeClient),
		exporter.NewOpenCostSource(newOpenCostClient(cfg, metrics, logger)),
		costStorage,
		cfg.ExporterInterval,
		logger,
		registry,
	)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go exp.Run(ctx)

	mux := http.NewServeMux()
	mux.Handle("/metrics", telemetry.Handler(registry))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		if !exp.Ready() {
			http.Error(w, "first refresh not finished", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	logger.Printf("Metrics on port %d\n", cfg.MetricsPort)

	serveUntilSignal(logger, []*http.Server{metricsServer(cfg.MetricsPort, mux)}, stop)
}

// newOpenCostClient creates the OpenCost client, timing its requests when metrics are on.
func newOpenCostClient(cfg *config.Config, metrics *telemetry.Metrics, logger *log.Logger) *opencost.Client {
	var opts []opencost.ClientOption
	if metrics != nil {
		// Validate has checked the URL.
		base, _ := url.Parse(cfg.OpenCost.BaseURL)
		opts = append(opts, opencost.WithTransport(metrics.InstrumentRoundTripper(base.Path, http.DefaultTransport)))
	}
	return opencost.NewClient(cfg.OpenCost.BaseURL, cfg.OpenCost.Timeout, logger, opts...)
}

// metricsServer serves handler on port. Scrapes are small and quick, so the timeouts are short.
func metricsServer(port int, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// serveUntilSignal serves every server until SIGINT or SIGTERM, then stops background work and shuts
// the servers down gracefully.
func serveUntilSignal(logger *log.Logger, servers []*http.Server, stopBackground func()) {
	for _, srv := range servers {
		go func() {
			logger.Printf("HTTP server starting on %s", srv.Addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Fatalf("Server error: %v", err)
			}
		}()
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	logger.Println("Shutdown signal received")

	stopBackground()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			logger.Printf("Server shutdown error: %v", err)
		}
	}
	logger.Println("Server stopped")
}
