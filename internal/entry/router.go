package entry

import (
	"github.com/gin-gonic/gin"
	"github.com/mmk31585/updater-service/internal/openapi"
)

func (s *Server) setupRouter() *gin.Engine {
	r := gin.Default()

	r.GET("/health", s.handleHealth)

	r.POST("/updates", s.handleCreateUpdate)
	r.GET("/updates/:id", s.handleGetUpdate)
	r.PUT("/updates/:id/file", s.handleUploadFile)
	r.GET("/operations", s.handleSearchOperations)

	r.GET("/internal/operations/:id/file", s.handleServeFile)

	openapi.Register(r)

	return r
}
