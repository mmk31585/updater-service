package entry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	natslib "github.com/nats-io/nats.go"
	"github.com/tus/tusd/v2/pkg/filelocker"
	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"

	"github.com/mmk31585/updater-service/internal/hash"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/subjects"
)

const (
	CommandSubject       = subjects.CommandPrefix
	ResultSubject        = subjects.Result
	NodeRegisterSubject  = subjects.NodeRegister
	NodeHeartbeatSubject = subjects.NodeHeartbeat
	NodeGoodbyeSubject   = subjects.NodeGoodbye
)

func internalBaseURL() string {
	if url := os.Getenv("INTERNAL_BASE_URL"); url != "" {
		return url
	}
	return "http://localhost:8080"
}

type Server struct {
	cfg         Config
	tusdOnce    sync.Once
	tusdHandler *handler.Handler
	tusdErr     error
}

func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) Handler() *gin.Engine {
	return s.setupRouter()
}

func (s *Server) getTusdHandler() (*handler.Handler, error) {
	s.tusdOnce.Do(func() {
		s.tusdHandler, s.tusdErr = s.newTusdHandler()
	})
	return s.tusdHandler, s.tusdErr
}

func (s *Server) tusdBasePath() string {
	basePath := s.cfg.TusdBasePath
	if basePath == "" {
		basePath = "/uploads"
	}
	return "/" + strings.Trim(basePath, "/")
}

func (s *Server) tusdUploadDir() string {
	if s.cfg.TusdUploadDir != "" {
		return s.cfg.TusdUploadDir
	}
	return filepath.Join(os.TempDir(), "update-entry-uploads")
}

func (s *Server) newTusdHandler() (*handler.Handler, error) {
	uploadDir := s.tusdUploadDir()
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, err
	}

	store := filestore.New(uploadDir)
	locker := filelocker.New(uploadDir)

	composer := handler.NewStoreComposer()
	store.UseIn(composer)
	locker.UseIn(composer)

	maxSize := s.cfg.TusdMaxSize
	if maxSize <= 0 {
		maxSize = 20 << 30
	}

	h, err := handler.NewHandler(handler.Config{
		BasePath:        s.tusdBasePath(),
		StoreComposer:   composer,
		MaxSize:         maxSize,
		DisableDownload: true,
		PreUploadCreateCallback: func(hook handler.HookEvent) (handler.HTTPResponse, handler.FileInfoChanges, error) {
			return s.onTusdUploadCreate(hook)
		},
		PreFinishResponseCallback: func(hook handler.HookEvent) (handler.HTTPResponse, error) {
			return s.onTusdUploadFinish(hook)
		},
	})
	if err != nil {
		return nil, err
	}

	return h, nil
}

func (s *Server) onTusdUploadCreate(hook handler.HookEvent) (handler.HTTPResponse, handler.FileInfoChanges, error) {
	operationID := strings.TrimSpace(hook.Upload.MetaData["operation_id"])
	if operationID == "" {
		return handler.HTTPResponse{StatusCode: http.StatusBadRequest}, handler.FileInfoChanges{}, fmt.Errorf("Upload-Metadata operation_id is required")
	}

	if s.cfg.OpRepo == nil {
		return handler.HTTPResponse{StatusCode: http.StatusInternalServerError}, handler.FileInfoChanges{}, fmt.Errorf("operation repository is required")
	}

	op, err := s.cfg.OpRepo.Get(hook.Context, operationID)
	if err != nil {
		if errors.Is(err, operation.ErrNotFound) {
			return handler.HTTPResponse{StatusCode: http.StatusNotFound}, handler.FileInfoChanges{}, nil
		}
		return handler.HTTPResponse{StatusCode: http.StatusInternalServerError}, handler.FileInfoChanges{}, fmt.Errorf("operation lookup failed: %w", err)
	}
	if op.Status != operation.StatusPending {
		return handler.HTTPResponse{StatusCode: http.StatusConflict}, handler.FileInfoChanges{}, nil
	}

	return handler.HTTPResponse{}, handler.FileInfoChanges{}, nil
}

func (s *Server) onTusdUploadFinish(hook handler.HookEvent) (handler.HTTPResponse, error) {
	info := hook.Upload

	operationID := strings.TrimSpace(info.MetaData["operation_id"])
	fileName := strings.TrimSpace(info.MetaData["filename"])
	fileName = filepath.Base(fileName)
	if fileName == "" || fileName == "." || fileName == "/" || fileName == ".." {
		fileName = "source.bin"
	}
	filePath := info.Storage[filestore.StorageKeyPath]

	if operationID == "" || filePath == "" {
		return handler.HTTPResponse{}, fmt.Errorf("missing operation_id or storage path on upload completion")
	}

	sum, err := hash.SHA256(filePath)
	if err != nil {
		return handler.HTTPResponse{}, fmt.Errorf("calculate sha256: %w", err)
	}

	ctx := hook.Context
	if ctx == nil {
		ctx = context.Background()
	}

	fileMeta := operation.FileMetadata{
		OperationID: operationID,
		FileName:    fileName,
		FilePath:    filePath,
		FileSize:    info.Size,
		SHA256:      sum,
		Status:      "READY",
		CreatedAt:   time.Now().UTC(),
	}

	if s.cfg.FileRepo == nil {
		return handler.HTTPResponse{}, fmt.Errorf("file repository is required")
	}
	if err := s.cfg.FileRepo.Create(ctx, fileMeta); err != nil {
		return handler.HTTPResponse{}, fmt.Errorf("save file metadata: %w", err)
	}

	op, err := s.cfg.OpRepo.Get(ctx, operationID)
	if err != nil {
		return handler.HTTPResponse{}, fmt.Errorf("load operation: %w", err)
	}

	if err := s.dispatchOperation(ctx, op, fileMeta); err != nil {
		return handler.HTTPResponse{}, fmt.Errorf("dispatch operation: %w", err)
	}

	if s.cfg.Logger != nil {
		s.cfg.Logger.Info("tusd upload finished",
			"operation_id", operationID,
			"file_name", fileName,
			"file_size", info.Size,
			"sha256", sum)
	}

	return handler.HTTPResponse{
		StatusCode: http.StatusNoContent,
		Header: handler.HTTPHeader{
			"Upload-Offset": fmt.Sprintf("%d", info.Size),
		},
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	if s.cfg.NATS == nil {
		return errors.New("NATS client is required")
	}
	if s.cfg.NodeRepo == nil {
		return errors.New("node repository is required")
	}
	if s.cfg.Logger == nil {
		s.cfg.Logger = slog.Default()
	}

	subscribe := func(subject string, handler func(*natslib.Msg)) error {
		if _, err := s.cfg.NATS.Subscribe(subject, handler); err != nil {
			return fmt.Errorf("subscribe to %s: %w", subject, err)
		}
		return nil
	}

	if err := subscribe(ResultSubject, s.handleResult); err != nil {
		return err
	}
	if err := subscribe(NodeRegisterSubject, s.handleNodeRegister); err != nil {
		return err
	}
	if err := subscribe(NodeHeartbeatSubject, s.handleNodeHeartbeat); err != nil {
		return err
	}
	if err := subscribe(NodeGoodbyeSubject, s.handleNodeGoodbye); err != nil {
		return err
	}

	if _, err := s.getTusdHandler(); err != nil {
		return fmt.Errorf("initialize tusd handler: %w", err)
	}

	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: s.cfg.ReadTimeout,
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.ListenAndServe()
	}()

	s.cfg.Logger.Info("entry started", "addr", s.cfg.Addr)

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err

	case <-ctx.Done():
		shutdownTimeout := s.cfg.ShutdownTimeout
		if shutdownTimeout <= 0 {
			shutdownTimeout = 10 * time.Second
		}
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			shutdownTimeout,
		)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown entry server: %w", err)
		}

		err := <-serveErr
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}

		s.cfg.Logger.Info("entry stopped")
		return nil
	}
}
