package entry

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	natslib "github.com/nats-io/nats.go"
)

const (
	internalBaseURL = "http://localhost:8080"
	CommandSubject  = "update.command.node-1"
	ResultSubject   = "update.result"
)

type Server struct {
	cfg Config
}

func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Handler returns the configured Gin engine with all routes registered.
// It is also used by tests to exercise the router without starting the
// HTTP listener.
func (s *Server) Handler() *gin.Engine {
	return s.setupRouter()
}

func (s *Server) Run(ctx context.Context) error {
	_, err := s.cfg.NATS.Subscribe(ResultSubject, func(msg *natslib.Msg) {
		s.handleResult(msg)
	})
	if err != nil {
		s.cfg.Logger.Error("failed to subscribe to result topic", "error", err)
		return err
	}

	router := s.Handler()

	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: s.cfg.ReadTimeout,
	}

	shutdownCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		s.cfg.Logger.Info("shutting down server")
		srvCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(srvCtx)
		close(shutdownCh)
	}()

	s.cfg.Logger.Info("entry started", "addr", s.cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		s.cfg.Logger.Error("http server failed", "error", err)
	}

	select {
	case <-shutdownCh:
	case <-time.After(15 * time.Second):
		s.cfg.Logger.Error("server shutdown timed out")
	}
	s.cfg.Logger.Info("entry stopped")
	return nil
}
