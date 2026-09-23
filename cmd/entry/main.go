package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mmk31585/updater-service/internal/config"
	"github.com/mmk31585/updater-service/internal/db/mariadb"
	"github.com/mmk31585/updater-service/internal/entry"
	"github.com/mmk31585/updater-service/internal/logging"
	"github.com/mmk31585/updater-service/internal/nats"

	// Registers the generated OpenAPI/Swagger specification so the
	// /swagger endpoints can serve the API documentation.
	_ "github.com/mmk31585/updater-service/internal/openapi"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/search"
	natslib "github.com/nats-io/nats.go"
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
	if err != nil {
		slog.New(slog.NewTextHandler(os.Stdout, nil)).Error("failed to load config", "error", err)
		os.Exit(1)
	}

	gin.SetMode(cnf.App.GinMode)

	logger := logging.NewLogger(cnf.App.AppName, cnf.App.AppEnv, cnf.Node.ID)

	nc, err := nats.New(nats.Config{URL: natslib.DefaultURL}, logger)
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

	searchClient, err := search.NewClient(cnf.Elastic.URL)
	if err != nil {
		logger.Error(
			"failed to create Elasticsearch client",
			"error", err,
		)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	if err := searchClient.EnsureIndex(ctx); err != nil {
		logger.Error(
			"failed to ensure Elasticsearch index",
			"error", err,
		)
		os.Exit(1)
	}

	logger.Info(
		"elasticsearch connected",
		"url", cnf.Elastic.URL,
	)

	srv := entry.New(entry.Config{
		Logger:          logger,
		NATS:            nc,
		OpRepo:          operation.NewMariaDBRepository(db),
		FileRepo:        operation.NewMariaDBFileRepository(db),
		StorageRoot:     cnf.Services.ServicesRoot,
		NodeID:          cnf.Node.ID,
		Addr:            ":8080",
		ReadTimeout:     cnf.HTTP.ReadTimeout,
		ShutdownTimeout: cnf.HTTP.ShutdownTimeout,
		SearchClient:    searchClient,
		MaxUploadSize:   config.ParseSize(cnf.File.MaxSize),
	})

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		logger.Error("database not reachable", "error", err)
		os.Exit(1)
	}

	logger.Info("database connected")
	logger.Info("entry starting", "addr", ":8080")

	if err := srv.Run(context.Background()); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
