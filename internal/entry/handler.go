package entry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/search"
	natslib "github.com/nats-io/nats.go"
)

const (
	uploadBufferSize      = 1024 * 1024
	defaultUploadFileName = "source.bin"
)

type Config struct {
	Logger          *slog.Logger
	NATS            *nats.Client
	OpRepo          operation.OperationRepository
	FileRepo        operation.FileRepository
	StorageRoot     string
	NodeID          string
	Addr            string
	ReadTimeout     time.Duration
	ShutdownTimeout time.Duration
	SearchClient    *search.Client
	MaxUploadSize   int64
}

func writeJSON(c *gin.Context, statusCode int, data any) {
	c.JSON(statusCode, data)
}

func advanceOperation(
	ctx context.Context,
	repo operation.OperationRepository,
	searchClient *search.Client,
	id string,
	status operation.Status,
	logger *slog.Logger,
) error {
	updated, err := repo.AdvanceStatus(ctx, id, status)
	if err != nil {
		return err
	}

	if !updated {
		return nil
	}

	op, err := repo.Get(ctx, id)
	if err != nil {
		return err
	}

	if searchClient != nil {
		if err := searchClient.IndexOperation(ctx, op); err != nil {
			logger.Error(
				"failed to index operation in Elasticsearch",
				"operation_id", id,
				"status", status,
				"error", err,
			)

			return nil
		}
	}

	return nil
}

// handleHealth reports the liveness of the service.
//
//	@Summary		Health check
//	@Description	Returns the health status of the service.
//	@Tags			Health
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Router			/health [get]
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{Status: "ok"})
}

// handleCreateUpdate registers a new update operation.
//
//	@Summary		Create an update operation
//	@Description	Creates a new pending update operation for a service and returns a unique
//	              operation id. The operation id is used to upload the update file
//	              (PUT /updates/{id}/file) and to track progress (GET /updates/{id}).
//	@Tags			Operations
//	@Accept			json
//	@Produce		json
//	@Param			request	body		CreateUpdateRequest	true	"Service to update"
//	@Success		202		{object}	CreateUpdateResponse
//	@Failure		400		{object}	ErrorResponse	"invalid request body or missing service"
//	@Failure		500		{object}	ErrorResponse	"failed to create the operation"
//	@Router			/updates [post]
func (s *Server) handleCreateUpdate(c *gin.Context) {
	var req CreateUpdateRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.Service == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service is required"})
		return
	}

	operationID, err := s.newOperationID()
	if err != nil {
		s.cfg.Logger.Error("failed to generate operation id", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create operation"})
		return
	}

	now := time.Now().UTC()

	op := operation.Operation{
		ID:        operationID,
		Status:    operation.StatusPending,
		Service:   req.Service,
		NodeID:    s.cfg.NodeID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.cfg.OpRepo.Create(c, op); err != nil {
		s.cfg.Logger.Error("failed to create operation", "operation_id", operationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create operation"})
		return
	}

	if s.cfg.SearchClient != nil {
		if err := s.cfg.SearchClient.IndexOperation(c, op); err != nil {
			s.cfg.Logger.Error("failed to index new operation", "operation_id", operationID, "error", err)
		}
	}

	c.JSON(http.StatusAccepted, CreateUpdateResponse{
		OperationID: operationID,
	})
}

// handleGetUpdate returns the current status of an update operation.
//
//	@Summary		Get update operation status
//	@Description	Returns the current status of the update operation identified by id.
//	@Tags			Operations
//	@Produce		json
//	@Param			id	path		string	true	"Operation ID"
//	@Success		200	{object}	OperationStatusResponse
//	@Failure		404	{object}	ErrorResponse	"operation not found"
//	@Router			/updates/{id} [get]
func (s *Server) handleGetUpdate(c *gin.Context) {
	id := c.Param("id")

	opLogger := s.cfg.Logger.With("operation_id", id)

	op, err := s.cfg.OpRepo.Get(c, id)
	if err != nil {
		opLogger.Error("operation not found")
		c.JSON(http.StatusNotFound, gin.H{"error": "operation not found"})
		return
	}

	c.JSON(http.StatusOK, OperationStatusResponse{
		ID:     op.ID,
		Status: string(op.Status),
	})
}

// handleUploadFile stores the update file for a pending operation.
//
//	@Summary		Upload an update file
//	@Description	Uploads the update file for a pending operation. Send it either as
//	              multipart/form-data with a "file" field or as a raw binary body
//	              (application/octet-stream). On success the operation is dispatched to
//	              the worker node for the requested service.
//	@Tags			Files
//	@Accept			multipart/form-data
//	@Produce		json
//	@Param			id		path		string	true	"Operation ID"
//	@Param			file	formData	file	true	"Update file to upload"
//	@Success		202		{object}	FileUploadResponse
//	@Failure		400		{object}	ErrorResponse	"missing file field or body too large"
//	@Failure		404		{object}	ErrorResponse	"operation not found"
//	@Failure		409		{object}	ErrorResponse	"operation is not pending"
//	@Failure		500		{object}	ErrorResponse	"storage or metadata failure"
//	@Failure		503		{object}	ErrorResponse	"failed to dispatch the operation"
//	@Router			/updates/{id}/file [put]
func (s *Server) handleUploadFile(c *gin.Context) {
	operationID := c.Param("id")

	op, err := s.cfg.OpRepo.Get(c, operationID)
	if err != nil {
		s.cfg.Logger.Error("operation not found", "operation_id", operationID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "operation not found"})
		return
	}

	if op.Status != operation.StatusPending {
		c.JSON(http.StatusConflict, gin.H{"error": "operation is not pending"})
		return
	}

	if s.cfg.MaxUploadSize > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, s.cfg.MaxUploadSize)
	}

	dir := filepath.Join(s.cfg.StorageRoot, "operations", operationID)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.cfg.Logger.Error("failed to create storage directory", "operation_id", operationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create storage directory"})
		return
	}

	var (
		src      io.Reader
		fileName string
	)

	isMultipart := strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data")

	switch {
	case isMultipart:
		header, err := c.FormFile("file")
		if err != nil {
			if tooLarge(err) {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file exceeds maximum upload size"})
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"error": `multipart field "file" is required`})
			}
			return
		}

		opened, err := header.Open()
		if err != nil {
			s.cfg.Logger.Error("failed to open uploaded file", "operation_id", operationID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read uploaded file"})
			return
		}
		defer opened.Close()

		src = opened
		fileName = filepath.Base(header.Filename)
	default:
		src = c.Request.Body
		fileName = defaultUploadFileName
	}

	tempPath := filepath.Join(dir, "source.part")
	finalPath := filepath.Join(dir, "source.bin")

	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		s.cfg.Logger.Error("failed to create file", "operation_id", operationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create file"})
		return
	}

	hasher := sha256.New()
	written, copyErr := io.CopyBuffer(io.MultiWriter(file, hasher), src, make([]byte, uploadBufferSize))

	if copyErr != nil {
		s.cfg.Logger.Error("file upload failed", "operation_id", operationID, "error", copyErr)
		_ = file.Close()
		_ = os.Remove(tempPath)
		if tooLarge(copyErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file exceeds maximum upload size"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file upload failed"})
		}
		return
	}

	if !isMultipart && c.Request.ContentLength >= 0 && written != c.Request.ContentLength {
		s.cfg.Logger.Error("uploaded size does not match Content-Length", "operation_id", operationID)
		_ = file.Close()
		_ = os.Remove(tempPath)
		c.JSON(http.StatusBadRequest, gin.H{"error": "uploaded size does not match Content-Length"})
		return
	}

	if err := file.Sync(); err != nil {
		s.cfg.Logger.Error("failed to sync file", "operation_id", operationID, "error", err)
		_ = file.Close()
		_ = os.Remove(tempPath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync file"})
		return
	}

	if err := file.Close(); err != nil {
		s.cfg.Logger.Error("failed to close file", "operation_id", operationID, "error", err)
		_ = os.Remove(tempPath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to close file"})
		return
	}

	sum := hex.EncodeToString(hasher.Sum(nil))

	if err := os.Rename(tempPath, finalPath); err != nil {
		s.cfg.Logger.Error("failed to finalize file", "operation_id", operationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to finalize file"})
		return
	}

	fileMeta := operation.FileMetadata{
		OperationID: operationID,
		FileName:    fileName,
		FilePath:    finalPath,
		FileSize:    written,
		SHA256:      sum,
		CreatedAt:   time.Now().UTC(),
	}

	if err := s.cfg.FileRepo.Create(c, fileMeta); err != nil {
		s.cfg.Logger.Error("failed to save file metadata", "operation_id", operationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file metadata"})
		return
	}

	if err := s.dispatchOperation(c, op, fileMeta); err != nil {
		s.cfg.Logger.Error("failed to dispatch operation", "operation_id", operationID, "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to dispatch operation"})
		return
	}

	c.JSON(http.StatusAccepted, FileUploadResponse{
		OperationID: operationID,
		FileName:    fileName,
		FileSize:    written,
		SHA256:      sum,
	})
}

// handleServeFile streams the uploaded update file back to worker nodes.
//
//	@Summary		Download the uploaded update file
//	@Description	Streams the update file for an operation. Supports HTTP range requests and is
//	              intended for worker nodes to download the file before applying an update.
//	@Tags			Files
//	@Produce		octet-stream
//	@Param			id	path	string	true	"Operation ID"
//	@Success		200	{file}	binary
//	@Failure		404
//	@Failure		416
//	@Router			/internal/operations/{id}/file [get]
func (s *Server) handleServeFile(c *gin.Context) {
	operationID := c.Param("id")

	meta, err := s.cfg.FileRepo.Get(c, operationID)
	if err != nil {
		s.cfg.Logger.Error("file not found", "operation_id", operationID, "error", err)
		c.Writer.WriteHeader(http.StatusNotFound)
		return
	}

	file, err := os.Open(meta.FilePath)
	if err != nil {
		s.cfg.Logger.Error("failed to open file", "operation_id", operationID, "error", err)
		c.Writer.WriteHeader(http.StatusNotFound)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		s.cfg.Logger.Error("failed to stat file", "operation_id", operationID, "error", err)
		c.Writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	fileSize := info.Size()

	c.Header("Accept-Ranges", "bytes")
	c.Header("ETag", fmt.Sprintf(`"%s"`, meta.SHA256))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, meta.FileName))

	rangeHeader := c.Request.Header.Get("Range")
	if rangeHeader == "" {
		c.DataFromReader(http.StatusOK, fileSize, "application/octet-stream", io.LimitReader(file, fileSize), nil)
		return
	}

	var start, end int64
	_, err = fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
	if err != nil || start < 0 || start >= fileSize {
		c.Header("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		c.Writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	if end >= fileSize {
		end = fileSize - 1
	}

	contentLength := end - start + 1

	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Header("Content-Length", fmt.Sprintf("%d", contentLength))

	if _, err := file.Seek(start, io.SeekStart); err != nil {
		s.cfg.Logger.Error("failed to seek file", "operation_id", operationID, "error", err)
		c.Writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	c.DataFromReader(http.StatusPartialContent, contentLength, "application/octet-stream", io.LimitReader(file, contentLength), nil)
}

// handleSearchOperations searches recorded update operations.
//
//	@Summary		Search update operations
//	@Description	Searches update operations indexed in Elasticsearch. Results can be filtered
//	              by service, status and node id and are paginated with limit and page.
//	@Tags			Operations
//	@Produce		json
//	@Param			service	query		string	false	"Filter by service name"	example("hello-service")
//	@Param			status	query		string	false	"Filter by status"			example("PENDING")	enums(PENDING,DISPATCHED,TRANSFERRED,RUNNING,BACKUP_CREATED,APPLYING,HEALTH_CHECKING,SUCCEEDED,FAILED,ROLLING_BACK,ROLLBACK_HEALTH_CHECKING,ROLLED_BACK,ROLLBACK_FAILED)
//	@Param			node_id	query		string	false	"Filter by worker node id"	example("node-1")
//	@Param			limit	query		int		false	"Maximum number of results"	default(20)	example(20)
//	@Param			page	query		int		false	"Page number (1-based)"		default(1)	example(1)
//	@Success		200		{object}	search.SearchResult
//	@Failure		503		{object}	ErrorResponse	"search temporarily unavailable"
//	@Router			/operations [get]
func (s *Server) handleSearchOperations(c *gin.Context) {
	query := c.Request.URL.Query()

	status := query.Get("status")
	service := query.Get("service")
	nodeID := query.Get("node_id")

	limit := 20

	if value := query.Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}

	page := 1

	if value := query.Get("page"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil &&
			parsed > 0 {
			page = parsed
		}
	}

	offset := (page - 1) * limit

	result, err := s.cfg.SearchClient.SearchOperations(
		c.Request.Context(),
		search.SearchFilter{
			Status:  status,
			Service: service,
			NodeID:  nodeID,
			Limit:   limit,
			Offset:  offset,
		},
	)
	if err != nil {
		writeJSON(
			c,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "search temporarily unavailable",
			},
		)
		return
	}

	writeJSON(
		c,
		http.StatusOK,
		result,
	)
}

func tooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func (s *Server) newOperationID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "op-" + hex.EncodeToString(b), nil
}

func (s *Server) handleResult(msg *natslib.Msg) {
	var result message.UpdateResult

	if err := json.Unmarshal(msg.Data, &result); err != nil {
		s.cfg.Logger.Error("failed to decode update result", "error", err)
		return
	}

	opLogger := s.cfg.Logger.With("operation_id", result.OperationID)

	opLogger.Info("received update result", "status", result.Status)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := advanceOperation(
		ctx,
		s.cfg.OpRepo,
		s.cfg.SearchClient,
		result.OperationID,
		operation.Status(result.Status),
		s.cfg.Logger,
	); err != nil {
		opLogger.Error(
			"failed to process operation result",
			"operation_id", result.OperationID,
			"error", err,
		)
		return
	}

	opLogger.Info(
		"operation result processed",
		"operation_id", result.OperationID,
		"status", result.Status,
	)
}

func (s *Server) dispatchOperation(c *gin.Context, op operation.Operation, meta operation.FileMetadata) error {
	if err := advanceOperation(
		c.Request.Context(),
		s.cfg.OpRepo,
		s.cfg.SearchClient,
		op.ID,
		operation.StatusDispatched,
		s.cfg.Logger,
	); err != nil {
		return err
	}

	cmd := message.UpdateCommand{
		OperationID: op.ID,
		Service:     op.Service,
		FileURL:     fmt.Sprintf("%s/internal/operations/%s/file", internalBaseURL(), op.ID),
		FileName:    meta.FileName,
		FileSize:    meta.FileSize,
		FileSHA256:  meta.SHA256,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	if err := s.cfg.NATS.Publish("update.command."+op.NodeID, data); err != nil {
		return err
	}

	if err := s.cfg.NATS.Flush(); err != nil {
		return err
	}

	return nil
}

type CreateUpdateRequest struct {
	Service string `json:"service" example:"hello-service" enums:"hello-service,config-service,test-service"`
}

type CreateUpdateResponse struct {
	OperationID string `json:"operation_id" example:"op-3f2a91c0"`
}

type OperationStatusResponse struct {
	ID     string `json:"id" example:"op-3f2a91c0"`
	Status string `json:"status" example:"PENDING" enums:"PENDING,DISPATCHED,TRANSFERRED,RUNNING,BACKUP_CREATED,APPLYING,HEALTH_CHECKING,SUCCEEDED,FAILED,ROLLING_BACK,ROLLBACK_HEALTH_CHECKING,ROLLED_BACK,ROLLBACK_FAILED"`
}

type FileUploadResponse struct {
	OperationID string `json:"operation_id" example:"op-3f2a91c0"`
	FileName    string `json:"file_name" example:"config.yaml"`
	FileSize    int64  `json:"file_size" example:"1048576"`
	SHA256      string `json:"sha256" example:"a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3"`
}

type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

type ErrorResponse struct {
	Error string `json:"error" example:"invalid request body"`
}
