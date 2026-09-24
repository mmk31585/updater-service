package entry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/node"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/search"
	natslib "github.com/nats-io/nats.go"
)

var errNodeUnavailable = errors.New("target node is unavailable")

type Config struct {
	Logger               *slog.Logger
	NATS                 *nats.Client
	OpRepo               operation.OperationRepository
	FileRepo             operation.FileRepository
	NodeRepo             node.Repository
	StorageRoot          string
	NodeID               string
	Addr                 string
	ReadTimeout          time.Duration
	ShutdownTimeout      time.Duration
	NodeHeartbeatTimeout time.Duration
	SearchClient         *search.Client
	MaxUploadSize        int64

	TusdUploadDir     string
	TusdMaxSize       int64
	TusdBasePath      string
	TusdNotifyTimeout time.Duration
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
//	@Description	Creates a new pending update operation for a service on a worker node and
//	              returns a unique operation id. Upload the update file via the tus
//	              resumable upload endpoint, then track progress (GET /updates/{id}).
//	@Tags			Operations
//	@Accept			json
//	@Produce		json
//	@Param			request	body		CreateUpdateRequest	true	"Service and target node"
//	@Success		202		{object}	CreateUpdateResponse
//	@Failure		400		{object}	ErrorResponse	"invalid request body or missing service/node_id"
//	@Failure		404		{object}	ErrorResponse	"node not found"
//	@Failure		409		{object}	ErrorResponse	"node is not online"
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
	if req.NodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "node_id is required"})
		return
	}
	if s.cfg.NodeRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "node registry unavailable"})
		return
	}

	targetNode, err := s.cfg.NodeRepo.Get(c.Request.Context(), req.NodeID)
	if errors.Is(err, node.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	if err != nil {
		s.cfg.Logger.Error("failed to get target node", "node_id", req.NodeID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get target node"})
		return
	}
	if targetNode.Status != node.StatusOnline {
		c.JSON(http.StatusConflict, gin.H{
			"error":  "target node is not online",
			"status": targetNode.Status,
		})
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
		NodeID:    req.NodeID,
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

func (s *Server) dispatchOperation(ctx context.Context, op operation.Operation, meta operation.FileMetadata) error {
	if s.cfg.NodeRepo == nil {
		return errors.New("node repository is required")
	}

	targetNode, err := s.cfg.NodeRepo.Get(ctx, op.NodeID)
	if err != nil {
		return err
	}
	if targetNode.Status != node.StatusOnline {
		return errNodeUnavailable
	}

	if err := advanceOperation(
		ctx,
		s.cfg.OpRepo,
		s.cfg.SearchClient,
		op.ID,
		operation.StatusDispatched,
		s.cfg.Logger,
	); err != nil {
		return err
	}

	cmd := message.UpdateCommand{
		OperationID:  op.ID,
		Service:      op.Service,
		NodeInstance: targetNode.InstanceID,
		FileURL:      fmt.Sprintf("%s/internal/operations/%s/file", internalBaseURL(), op.ID),
		FileName:     meta.FileName,
		FileSize:     meta.FileSize,
		FileSHA256:   meta.SHA256,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	if err := s.cfg.NATS.Publish(CommandSubject+op.NodeID, data); err != nil {
		return err
	}

	if err := s.cfg.NATS.Flush(); err != nil {
		return err
	}

	return nil
}

func (s *Server) handleNodeRegister(msg *natslib.Msg) {
	if err := processNodeRegister(msg, s.cfg.NodeRepo, s.cfg.NodeHeartbeatTimeout); err != nil {
		s.cfg.Logger.Error("failed to process node registration", "error", err)
	}
}

func (s *Server) handleNodeHeartbeat(msg *natslib.Msg) {
	if err := processNodeHeartbeat(msg, s.cfg.NodeRepo, s.cfg.NodeHeartbeatTimeout); err != nil {
		s.cfg.Logger.Error("failed to process node heartbeat", "error", err)
	}
}

func (s *Server) handleNodeGoodbye(msg *natslib.Msg) {
	if err := processNodeGoodbye(msg, s.cfg.NodeRepo); err != nil {
		s.cfg.Logger.Error("failed to process node goodbye", "error", err)
	}
}

func processNodeRegister(
	msg *natslib.Msg,
	repo node.Repository,
	lease time.Duration,
) error {
	if repo == nil {
		return errors.New("node repository is required")
	}
	if msg == nil {
		return errors.New("node registration message is required")
	}

	var payload message.NodeRegister
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return fmt.Errorf("decode node registration: %w", err)
	}

	nodeID := strings.TrimSpace(payload.NodeID)
	instanceID := strings.TrimSpace(payload.InstanceID)
	if nodeID == "" || instanceID == "" {
		return errors.New("node_id and instance_id are required")
	}

	startedAt, err := time.Parse(time.RFC3339Nano, payload.StartedAt)
	if err != nil {
		return fmt.Errorf("parse started_at: %w", err)
	}
	if lease <= 0 {
		lease = 15 * time.Second
	}

	now := time.Now().UTC()
	return repo.Register(context.Background(), node.Node{
		ID:             nodeID,
		InstanceID:     instanceID,
		Status:         node.StatusOnline,
		Address:        payload.Address,
		Version:        payload.Version,
		Capabilities:   payload.Capabilities,
		LastSeenAt:     now,
		LeaseExpiresAt: now.Add(lease),
		StartedAt:      startedAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
}

func processNodeHeartbeat(
	msg *natslib.Msg,
	repo node.Repository,
	lease time.Duration,
) error {
	if repo == nil {
		return errors.New("node repository is required")
	}
	if msg == nil {
		return errors.New("node heartbeat message is required")
	}

	var payload message.NodeHeartbeat
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return fmt.Errorf("decode node heartbeat: %w", err)
	}

	nodeID := strings.TrimSpace(payload.NodeID)
	instanceID := strings.TrimSpace(payload.InstanceID)
	if nodeID == "" || instanceID == "" {
		return errors.New("node_id and instance_id are required")
	}
	if lease <= 0 {
		lease = 15 * time.Second
	}

	return repo.Heartbeat(
		context.Background(),
		nodeID,
		instanceID,
		time.Now().UTC().Add(lease),
	)
}

func processNodeGoodbye(msg *natslib.Msg, repo node.Repository) error {
	if repo == nil {
		return errors.New("node repository is required")
	}
	if msg == nil {
		return errors.New("node goodbye message is required")
	}

	var payload message.NodeGoodbye
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return fmt.Errorf("decode node goodbye: %w", err)
	}

	nodeID := strings.TrimSpace(payload.NodeID)
	instanceID := strings.TrimSpace(payload.InstanceID)
	if nodeID == "" || instanceID == "" {
		return errors.New("node_id and instance_id are required")
	}

	return repo.Goodbye(context.Background(), nodeID, instanceID)
}

func RunNodeMonitor(
	ctx context.Context,
	repo node.Repository,
	interval time.Duration,
	logger *slog.Logger,
) {
	if repo == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := repo.MarkExpired(ctx, now.UTC()); err != nil {
				logger.Error("failed to expire nodes", "error", err)
			}
		}
	}
}

func (s *Server) List(c *gin.Context) {
	if s.cfg.NodeRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "node registry unavailable"})
		return
	}

	items, err := s.cfg.NodeRepo.List(c.Request.Context())
	if err != nil {
		s.cfg.Logger.Error("failed to list nodes", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list nodes"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": len(items),
	})
}

func (s *Server) Get(c *gin.Context) {
	if s.cfg.NodeRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "node registry unavailable"})
		return
	}

	id := c.Param("id")
	record, err := s.cfg.NodeRepo.Get(c.Request.Context(), id)
	if errors.Is(err, node.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	if err != nil {
		s.cfg.Logger.Error("failed to get node", "node_id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get node"})
		return
	}

	c.JSON(http.StatusOK, record)
}

func (s *Server) Drain(c *gin.Context) {
	s.setDraining(c, true)
}

func (s *Server) Undrain(c *gin.Context) {
	s.setDraining(c, false)
}

func (s *Server) setDraining(c *gin.Context, draining bool) {
	if s.cfg.NodeRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "node registry unavailable"})
		return
	}

	id := c.Param("id")
	record, err := s.cfg.NodeRepo.Get(c.Request.Context(), id)
	if errors.Is(err, node.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	if err != nil {
		s.cfg.Logger.Error("failed to get node", "node_id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get node"})
		return
	}
	if record.Status == node.StatusOffline {
		c.JSON(http.StatusConflict, gin.H{"error": "node is offline"})
		return
	}

	if err := s.cfg.NodeRepo.SetDraining(c.Request.Context(), id, draining); err != nil {
		if errors.Is(err, node.ErrNodeOffline) {
			c.JSON(http.StatusConflict, gin.H{"error": "node is offline"})
			return
		}
		s.cfg.Logger.Error("failed to update node drain state", "node_id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update node"})
		return
	}

	status := node.StatusOnline
	if draining {
		status = node.StatusDraining
	}
	c.JSON(http.StatusOK, gin.H{
		"node_id": id,
		"status":  status,
	})
}

type CreateUpdateRequest struct {
	Service string `json:"service" example:"hello-service" enums:"hello-service,config-service,test-service"`
	NodeID  string `json:"node_id" example:"worker-node"`
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
