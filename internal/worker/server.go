package worker

import (
	"context"
	"log/slog"

	"github.com/mmk31585/updater-service/internal/config"
	"github.com/mmk31585/updater-service/internal/docker"
	"github.com/mmk31585/updater-service/internal/fileops"
	"github.com/mmk31585/updater-service/internal/filetransfer"
	"github.com/mmk31585/updater-service/internal/healthcheck"
	"github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/search"
	natslib "github.com/nats-io/nats.go"
)

const (
	CommandSubject = "update.command.node-1"
	ResultSubject  = "update.result"
)

type Config struct {
	Logger        *slog.Logger
	NATS          *nats.Client
	OpRepo        operation.OperationRepository
	Downloader    *filetransfer.Downloader
	DockerRunner  *docker.Runner
	HealthChecker *healthcheck.Checker
	FileDeployer  *fileops.Deployer
	StorageRoot   string
	OperationCfg  config.OperationConfig
	SearchClient  *search.Client
}

type Server struct {
	cfg Config
}

func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) Run(ctx context.Context) error {
	_, err := s.cfg.NATS.Subscribe(CommandSubject, func(msg *natslib.Msg) {
		s.handleCommand(msg)
	})
	if err != nil {
		s.cfg.Logger.Error("failed to subscribe", "error", err)
		return err
	}

	s.cfg.Logger.Info("worker started", "subject", CommandSubject)

	<-ctx.Done()

	s.cfg.Logger.Info("worker shutting down")
	return nil
}

func (s *Server) advanceStatus(
	ctx context.Context,
	operationID string,
	status operation.Status,
) bool {
	updated, err := s.cfg.OpRepo.AdvanceStatus(ctx, operationID, status)
	if err != nil {
		s.cfg.Logger.Error("failed to update status", "error", err)
		return false
	}
	if !updated {
		return false
	}

	op, err := s.cfg.OpRepo.Get(ctx, operationID)
	if err != nil {
		s.cfg.Logger.Error("failed to get operation", "error", err)
		return false
	}

	if err := s.cfg.SearchClient.IndexOperation(ctx, op); err != nil {
		s.cfg.Logger.Error("failed to index in Elasticsearch", "error", err)
	}

	return true
}
