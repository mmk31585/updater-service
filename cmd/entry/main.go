package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mmk31585/updater-service/internal/config"
	"github.com/mmk31585/updater-service/internal/db/mariadb"
	"github.com/mmk31585/updater-service/internal/entry"
	"github.com/mmk31585/updater-service/internal/logging"
	"github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/node"

	// Registers the generated OpenAPI/Swagger specification so the
	// /swagger endpoints can serve the API documentation.
	_ "github.com/mmk31585/updater-service/internal/openapi"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/search"
)

// Updater Service API
//
//	@title			Updater Service API
//	@version		1.0.0
//	@description	Deploys configuration and binary updates to managed services. Clients create
//
//	an update operation, upload the update file, then track the operation until
//	it reaches a terminal state (SUCCEEDED, FAILED, ROLLED_BACK, ROLLBACK_FAILED).
//
//	@termsOfService	http://swagger.io/terms/
//	@host			localhost:8080
//	@basePath		/
//	@schemes		http
func main() {
	cnf, err := config.LoadConfig()
	if err == nil {
		err = cnf.Validate()
	}
	if err != nil {
		slog.New(slog.NewTextHandler(os.Stdout, nil)).Error("failed to load config", "error", err)
		os.Exit(1)
	}

	gin.SetMode(cnf.App.GinMode)

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

	if err := mariadb.Migrate(db, os.Getenv("MIGRATIONS_DIR")); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	var searchClient *search.Client

	if cnf.Elastic.Enabled {
		client, err := search.NewClient(cnf.Elastic.URL)
		if err != nil {
			logger.Error(
				"failed to create Elasticsearch client",
				"error", err,
			)
			os.Exit(1)
		}

		if err := waitForElasticsearch(client, logger); err != nil {
			logger.Error(
				"failed to ensure Elasticsearch index",
				"error", err,
			)
			os.Exit(1)
		}

		searchClient = client
		logger.Info(
			"elasticsearch connected",
			"url", cnf.Elastic.URL,
		)
	} else {
		logger.Info(
			"elasticsearch disabled",
		)
	}

	nodeRepo := node.NewMariaDBNodeRepository(db)
	srv := entry.New(entry.Config{
		Logger:               logger,
		NATS:                 nc,
		OpRepo:               operation.NewMariaDBRepository(db),
		FileRepo:             operation.NewMariaDBFileRepository(db),
		NodeRepo:             nodeRepo,
		StorageRoot:          cnf.Services.ServicesRoot,
		NodeID:               cnf.Node.ID,
		Addr:                 net.JoinHostPort(cnf.HTTP.Host, cnf.HTTP.Port),
		ReadTimeout:          cnf.HTTP.ReadTimeout,
		ShutdownTimeout:      cnf.HTTP.ShutdownTimeout,
		NodeHeartbeatTimeout: cnf.Node.HeartbeatTimeout,
		SearchClient:         searchClient,
		MaxUploadSize:        config.ParseSize(cnf.File.MaxSize),
		TusdUploadDir:        cnf.File.TusdUploadDir,
		TusdMaxSize:          config.ParseSize(cnf.File.TusdMaxSize),
		TusdBasePath:         cnf.File.TusdBasePath,
		TusdNotifyTimeout:    cnf.File.TusdNotifyTimeout,
	})

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(pingCtx); err != nil {
		logger.Error("database not reachable", "error", err)
		os.Exit(1)
	}

	logger.Info("database connected")

	runCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	go entry.RunNodeMonitor(
		runCtx,
		nodeRepo,
		cnf.Node.HeartbeatInterval,
		logger,
	)

	logger.Info("entry starting", "addr", ":"+cnf.HTTP.Port)

	if err := srv.Run(runCtx); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func waitForElasticsearch(client *search.Client, logger *slog.Logger) error {
	var lastErr error
	for attempt := 1; attempt <= 15; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		lastErr = client.EnsureIndex(ctx)
		cancel()

		if lastErr == nil {
			return nil
		}

		logger.Warn(
			"elasticsearch not ready",
			"attempt", attempt,
			"error", lastErr,
		)
		time.Sleep(2 * time.Second)
	}
	return lastErr
}
