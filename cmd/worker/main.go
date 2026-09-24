package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mmk31585/updater-service/internal/config"
	"github.com/mmk31585/updater-service/internal/db/mariadb"
	"github.com/mmk31585/updater-service/internal/docker"
	"github.com/mmk31585/updater-service/internal/fileops"
	"github.com/mmk31585/updater-service/internal/filetransfer"
	"github.com/mmk31585/updater-service/internal/healthcheck"
	"github.com/mmk31585/updater-service/internal/heartbeat"
	"github.com/mmk31585/updater-service/internal/logging"
	"github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/retry"
	"github.com/mmk31585/updater-service/internal/search"
	"github.com/mmk31585/updater-service/internal/worker"
)

func main() {
	cnf, err := config.LoadConfig()
	if err == nil {
		err = cnf.Validate()
	}
	if err != nil {
		slog.New(slog.NewTextHandler(os.Stdout, nil)).Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := logging.NewLogger(cnf.App.AppName, cnf.App.AppEnv, cnf.Node.ID)

	nc, err := nats.New(nats.Config{
		URL:             cnf.Nats.URL,
		ConnectTimeout:  cnf.Nats.ConnectTimeout,
		ReconnectPeriod: cnf.Nats.ReconnectPeriod,
		MaxReconnect:    cnf.Nats.MaxReconnect,
		DrainTimeout:    cnf.Nats.DrainTimeout,
	}, logger)
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

	if err := waitForMigrations(db, os.Getenv("MIGRATIONS_DIR"), logger); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	monitor := operation.NewTimeoutMonitor(
		db,
		cnf.Operation.OperationTimeout,
		1*time.Minute,
	)
	go monitor.Run(context.Background())

	var searchClient *search.Client

	if cnf.Elastic.Enabled {
		client, err := search.NewClient(cnf.Elastic.URL)
		if err != nil {
			logger.Error("failed to create Elasticsearch client", "error", err)
			os.Exit(1)
		}
		searchClient = client
	} else {
		logger.Info("elasticsearch disabled")
	}

	heartbeatManager := heartbeat.NewHeartbeatManager(
		nc,
		cnf.Node.ID,
		cnf.Node.IncarnationID,
		cnf.Node.Address,
		cnf.Node.Version,
		nil,
		cnf.Node.HeartbeatInterval,
		cnf.Node.HeartbeatTimeout,
		logger,
	)

	srv := worker.New(worker.Config{
		Logger: logger,
		NATS:   nc,
		OpRepo: operation.NewMariaDBRepository(db),
		Downloader: filetransfer.NewDownloader(
			logger.With("component", "downloader"),
			config.ParseSize(cnf.File.DownloadChunkSize),
			cnf.File.DownloadConcurrency,
			cnf.File.DownloadMaxRetries,
		),
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
		NodeID:       cnf.Node.ID,
		Heartbeat:    heartbeatManager,
	})

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	logger.Info("worker starting")

	if err := srv.Run(ctx); err != nil {
		logger.Error("worker failed", "error", err)
		os.Exit(1)
	}
}

func waitForMigrations(db *sql.DB, dir string, logger *slog.Logger) error {
	var lastErr error
	for attempt := 1; attempt <= 12; attempt++ {
		lastErr = mariadb.Migrate(db, dir)
		if lastErr == nil {
			return nil
		}
		logger.Warn(
			"migration attempt failed",
			"attempt", attempt,
			"error", lastErr,
		)
		time.Sleep(2 * time.Second)
	}
	return lastErr
}
