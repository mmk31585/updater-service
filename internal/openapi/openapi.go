// Package openapi serves the Swagger UI and the generated OpenAPI documents
// that describe the Updater Service HTTP API.
//
// The specification is generated with swag in the Makefile:
//
//	swag init -g cmd/entry/main.go -o internal/openapi
//
// and is embedded into the binary so the UI keeps working without any
// external files at runtime.
package openapi

import (
	"embed"
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

//go:embed swagger.json
//go:embed swagger.yaml
var specFS embed.FS

// Handler returns a Gin handler that serves the Swagger UI together with the
// generated specification (fetched by the UI from doc.json). Mount with a
// wildcard route, e.g. GET /swagger/*any.
func Handler() gin.HandlerFunc {
	return ginSwagger.WrapHandler(
		swaggerFiles.Handler,
		ginSwagger.DeepLinking(true),
		ginSwagger.DocExpansion("list"),
	)
}

// RedirectHandler redirects GET /swagger to the Swagger UI index page.
func RedirectHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	}
}

// JSONHandler serves the generated OpenAPI document (swagger.json).
func JSONHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		serveSpec(c, "swagger.json", "application/json; charset=utf-8")
	}
}

// YAMLHandler serves the generated OpenAPI document (swagger.yaml).
func YAMLHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		serveSpec(c, "swagger.yaml", "application/yaml; charset=utf-8")
	}
}

func serveSpec(c *gin.Context, name, contentType string) {
	data, err := specFS.ReadFile(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "openapi document unavailable"})
		return
	}

	c.Data(http.StatusOK, contentType, data)
}

// Register mounts the Swagger UI and the generated OpenAPI documents on the
// provided router:
//
//	GET /swagger        -> 301 redirect to /swagger/index.html
//	GET /swagger/*any   -> Swagger UI and doc.json
//	GET /swagger.json   -> generated OpenAPI document as JSON
//	GET /swagger.yaml   -> generated OpenAPI document as YAML
func Register(r *gin.Engine) {
	r.GET("/swagger", RedirectHandler())
	r.GET("/swagger/*any", Handler())
	r.GET("/swagger.json", JSONHandler())
	r.GET("/swagger.yaml", YAMLHandler())
}
