package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/mmk31585/updater-service/internal/config"
	"github.com/mmk31585/updater-service/internal/db/mariadb"
	"github.com/mmk31585/updater-service/internal/docker"
	"github.com/mmk31585/updater-service/internal/fileops"
	"github.com/mmk31585/updater-service/internal/filetransfer"
	"github.com/mmk31585/updater-service/internal/healthcheck"
	"github.com/mmk31585/updater-service/internal/logging"
	nat "github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/retry"
	"github.com/mmk31585/updater-service/internal/search"
	"github.com/mmk31585/updater-service/internal/worker"
	natslib "github.com/nats-io/nats.go"
)

func main() {
	cnf, err := config.LoadConfig()
	if err != nil {
		slog.New(slog.NewTextHandler(os.Stdout, nil)).Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := logging.NewLogger(cnf.App.AppName, cnf.App.AppEnv, cnf.Node.ID)

	nc, err := nat.New(nat.Config{URL: natslib.DefaultURL}, logger)
	if err != nil {
		logger.Error("failed to connect to NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	db, err := mariadb.New(&cnf.DB)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := mariadb.Migrate(db, os.Getenv("MIGRATIONS_DIR")); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	monitor := operation.NewTimeoutMonitor(
		db,
		cnf.Operation.OperationTimeout,
		1*time.Minute,
	)
	go monitor.Run(context.Background())

	elasticsearchURL := os.Getenv("ELASTICSEARCH_URL")
	if elasticsearchURL == "" {
		elasticsearchURL = cnf.Elastic.URL
	}

	searchClient, err := search.NewClient(elasticsearchURL)
	if err != nil {
		logger.Error("failed to create Elasticsearch client", "error", err)
		os.Exit(1)
	}

	srv := worker.New(worker.Config{
		Logger:       logger,
		NATS:         nc,
		OpRepo:       operation.NewMariaDBRepository(db),
		Downloader:   filetransfer.NewDownloader(),
		DockerRunner: docker.NewRunner(cnf.Services.ServicesRoot),
		HealthChecker: healthcheck.NewChecker(healthcheck.Config{
			TotalTimeout:   cnf.Operation.HealthCheckTimeout,
			RequestTimeout: cnf.Operation.HealthRequestTimeout,
			MaxRetries:     cnf.Operation.MaxHealthRetries,
			Backoff: retry.Backoff{
				Initial: cnf.Operation.RetryInitialDelay,
				Max:     cnf.Operation.RetryMaxDelay,
			},
		}),
		FileDeployer: fileops.NewDeployer(),
		StorageRoot:  cnf.Services.ServicesRoot,
		OperationCfg: cnf.Operation,
		SearchClient: searchClient,
	})

	logger.Info("worker starting")

	if err := srv.Run(context.Background()); err != nil {
		logger.Error("worker failed", "error", err)
		os.Exit(1)
	}
}
