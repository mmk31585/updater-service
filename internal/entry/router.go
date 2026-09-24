package entry

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mmk31585/updater-service/internal/openapi"
)

func (s *Server) setupRouter() *gin.Engine {
	r := gin.Default()

	r.GET("/health", s.handleHealth)

	updates := r.Group("/updates")
	{
		updates.POST("", s.handleCreateUpdate)
		updates.GET("/:id", s.handleGetUpdate)
	}

	r.GET("/operations", s.handleSearchOperations)

	files := r.Group("/internal/operations")
	{
		files.GET("/:id/file", s.handleServeFile)
		files.HEAD("/:id/file", s.handleServeFile)
	}

	nodes := r.Group("/nodes")
	{
		nodes.GET("", s.List)
		nodes.GET("/:id", s.Get)
		nodes.POST("/:id/drain", s.Drain)
		nodes.POST("/:id/undrain", s.Undrain)
	}

	s.registerUploadRoutes(r)

	openapi.Register(r)

	return r
}

// registerUploadRoutes mounts the tus resumable upload handler. Both the
// collection endpoint (POST /uploads) and the per-upload endpoints
// (PATCH/DELETE /uploads/:id) are served by the same handler under the
// configured base path.
func (s *Server) registerUploadRoutes(r *gin.Engine) {
	h, err := s.getTusdHandler()
	if err != nil {
		if s.cfg.Logger != nil {
			s.cfg.Logger.Error("failed to initialize tusd upload handler", "error", err)
		}
		return
	}

	basePath := s.tusdBasePath()

	if s.cfg.Logger != nil {
		s.cfg.Logger.Info("tusd upload handler enabled", "path", basePath)
	}

	stripped := http.StripPrefix(basePath, h)
	r.Any(basePath, gin.WrapH(stripped))
	r.Any(basePath+"/*filepath", gin.WrapH(stripped))
}
