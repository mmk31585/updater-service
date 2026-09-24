package worker

import (
	"context"
	"log/slog"

	"github.com/mmk31585/updater-service/internal/config"
	"github.com/mmk31585/updater-service/internal/heartbeat"
	"github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/search"
	"github.com/mmk31585/updater-service/internal/services"
	"github.com/mmk31585/updater-service/internal/subjects"
	natslib "github.com/nats-io/nats.go"
)

type Downloader interface {
	Download(
		ctx context.Context,
		url string,
		dest string,
		expectedSize int64,
		expectedSHA256 string,
	) error
}

type DockerRunner interface {
	Service(service string) (services.ServiceDefinition, bool)
	Restart(ctx context.Context, service string) error
}

type HealthChecker interface {
	WaitUntilHealthy(ctx context.Context, url string) error
}

type FileDeployer interface {
	Backup(target string, backup string) (bool, error)
	Apply(staged string, target string) error
	Rollback(backup string, target string) error
}

const (
	CommandSubject       = subjects.CommandPrefix
	ResultSubject        = subjects.Result
	NodeRegisterSubject  = subjects.NodeRegister
	NodeHeartbeatSubject = subjects.NodeHeartbeat
	NodeGoodbyeSubject   = subjects.NodeGoodbye
)

type Config struct {
	Logger        *slog.Logger
	NATS          *nats.Client
	OpRepo        operation.OperationRepository
	Downloader    Downloader
	DockerRunner  DockerRunner
	HealthChecker HealthChecker
	FileDeployer  FileDeployer
	StorageRoot   string
	OperationCfg  config.OperationConfig
	SearchClient  *search.Client
	NodeID        string
	Heartbeat     *heartbeat.HeartbeatManager
}

type Server struct {
	cfg Config
}

func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) Run(ctx context.Context) error {
	commandSubject := CommandSubject + s.cfg.NodeID
	_, err := s.cfg.NATS.Subscribe(commandSubject, func(msg *natslib.Msg) {
		s.handleCommand(msg)
	})
	if err != nil {
		s.cfg.Logger.Error("failed to subscribe", "error", err)
		return err
	}

	s.cfg.Logger.Info("worker started", "subject", commandSubject)

	var heartbeatDone chan struct{}
	if s.cfg.Heartbeat != nil {
		heartbeatDone = make(chan struct{})
		go func() {
			defer close(heartbeatDone)
			s.cfg.Heartbeat.Run(ctx)
		}()
	}

	<-ctx.Done()

	if heartbeatDone != nil {
		<-heartbeatDone
	}

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

	if s.cfg.SearchClient != nil {
		if err := s.cfg.SearchClient.IndexOperation(ctx, op); err != nil {
			s.cfg.Logger.Error("failed to index in Elasticsearch", "error", err)
		}
	}

	return true
}
