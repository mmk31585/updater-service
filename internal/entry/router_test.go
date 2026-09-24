package entry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/node"
	"github.com/mmk31585/updater-service/internal/operation"
	natslib "github.com/nats-io/nats.go"
)

func newTestServer() *Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Config{Logger: logger})
}

func TestSwaggerRoutes(t *testing.T) {
	router := newTestServer().Handler()

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "swagger ui", method: http.MethodGet, path: "/swagger/index.html", wantStatus: http.StatusOK},
		{name: "swagger spec json", method: http.MethodGet, path: "/swagger/doc.json", wantStatus: http.StatusOK},
		{name: "raw swagger.json", method: http.MethodGet, path: "/swagger.json", wantStatus: http.StatusOK},
		{name: "raw swagger.yaml", method: http.MethodGet, path: "/swagger.yaml", wantStatus: http.StatusOK},
		{name: "health", method: http.MethodGet, path: "/health", wantStatus: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("%s %s returned %d, want %d", tc.method, tc.path, w.Code, tc.wantStatus)
			}
		})
	}
}

func TestSwaggerRedirect(t *testing.T) {
	router := newTestServer().Handler()

	req := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /swagger returned %d, want %d", w.Code, http.StatusMovedPermanently)
	}

	if got := w.Header().Get("Location"); got != "/swagger/index.html" {
		t.Fatalf("location = %q, want %q", got, "/swagger/index.html")
	}
}

func TestSwaggerDocIncludesEndpoints(t *testing.T) {
	router := newTestServer().Handler()

	req := httptest.NewRequest(http.MethodGet, "/swagger/doc.json", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	for _, endpoint := range []string{
		`"/updates"`,
		`"/updates/{id}"`,
		`"/operations"`,
		`"/internal/operations/{id}/file"`,
		`"/uploads"`,
		`"/uploads/{id}"`,
		`"/health"`,
	} {
		if !strings.Contains(w.Body.String(), endpoint) {
			t.Errorf("doc.json does not contain endpoint %s", endpoint)
		}
	}
}

type memoryNodeRepository struct {
	records map[string]node.Node
}

func newMemoryNodeRepository(records ...node.Node) *memoryNodeRepository {
	repo := &memoryNodeRepository{records: make(map[string]node.Node)}
	for _, record := range records {
		repo.records[record.ID] = record
	}
	return repo
}

func (r *memoryNodeRepository) Register(_ context.Context, record node.Node) error {
	r.records[record.ID] = record
	return nil
}

func (r *memoryNodeRepository) Heartbeat(
	_ context.Context,
	nodeID string,
	instanceID string,
	leaseExpiresAt time.Time,
) error {
	record, ok := r.records[nodeID]
	if !ok || record.InstanceID != instanceID {
		return node.ErrStaleInstance
	}
	record.LastSeenAt = time.Now().UTC()
	record.LeaseExpiresAt = leaseExpiresAt
	if record.Status != node.StatusDraining {
		record.Status = node.StatusOnline
	}
	r.records[nodeID] = record
	return nil
}

func (r *memoryNodeRepository) Goodbye(
	_ context.Context,
	nodeID string,
	instanceID string,
) error {
	record, ok := r.records[nodeID]
	if !ok || record.InstanceID != instanceID {
		return node.ErrStaleInstance
	}
	record.Status = node.StatusOffline
	now := time.Now().UTC()
	record.LastSeenAt = now
	record.LeaseExpiresAt = now
	r.records[nodeID] = record
	return nil
}

func (r *memoryNodeRepository) MarkExpired(_ context.Context, now time.Time) error {
	for id, record := range r.records {
		if record.Status != node.StatusOffline && !record.LeaseExpiresAt.After(now) {
			record.Status = node.StatusOffline
			r.records[id] = record
		}
	}
	return nil
}

func (r *memoryNodeRepository) Get(_ context.Context, id string) (node.Node, error) {
	record, ok := r.records[id]
	if !ok {
		return node.Node{}, node.ErrNotFound
	}
	return record, nil
}

func (r *memoryNodeRepository) List(_ context.Context) ([]node.Node, error) {
	records := make([]node.Node, 0, len(r.records))
	for _, record := range r.records {
		records = append(records, record)
	}
	return records, nil
}

func (r *memoryNodeRepository) SetDraining(
	_ context.Context,
	id string,
	draining bool,
) error {
	record, ok := r.records[id]
	if !ok {
		return node.ErrNotFound
	}
	if record.Status == node.StatusOffline {
		return node.ErrNodeOffline
	}
	if draining {
		record.Status = node.StatusDraining
	} else {
		record.Status = node.StatusOnline
	}
	r.records[id] = record
	return nil
}

type memoryOperationRepository struct {
	records map[string]operation.Operation
}

func newMemoryOperationRepository() *memoryOperationRepository {
	return &memoryOperationRepository{records: make(map[string]operation.Operation)}
}

func (r *memoryOperationRepository) Create(_ context.Context, op operation.Operation) error {
	r.records[op.ID] = op
	return nil
}

func (r *memoryOperationRepository) Get(_ context.Context, id string) (operation.Operation, error) {
	op, ok := r.records[id]
	if !ok {
		return operation.Operation{}, fmt.Errorf("operation %s not found", id)
	}
	return op, nil
}

func (r *memoryOperationRepository) AdvanceStatus(
	_ context.Context,
	id string,
	status operation.Status,
) (bool, error) {
	op, ok := r.records[id]
	if !ok {
		return false, nil
	}
	op.Status = status
	op.UpdatedAt = time.Now().UTC()
	r.records[id] = op
	return true, nil
}

func (r *memoryOperationRepository) ClaimForExecution(
	ctx context.Context,
	id string,
) (bool, operation.Operation, error) {
	op, err := r.Get(ctx, id)
	return false, op, err
}

func (r *memoryOperationRepository) ListAll(_ context.Context) ([]operation.Operation, error) {
	records := make([]operation.Operation, 0, len(r.records))
	for _, op := range r.records {
		records = append(records, op)
	}
	return records, nil
}

func newNodeTestServer(
	t *testing.T,
	nodeRepo node.Repository,
	opRepo operation.OperationRepository,
) *Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Config{
		Logger:        logger,
		NodeRepo:      nodeRepo,
		OpRepo:        opRepo,
		TusdUploadDir: t.TempDir(),
	})
}

func TestCreateUpdateRequiresOnlineTargetNode(t *testing.T) {
	online := node.Node{ID: "worker-node", InstanceID: "instance-1", Status: node.StatusOnline}
	draining := node.Node{ID: "worker-node", InstanceID: "instance-1", Status: node.StatusDraining}

	tests := []struct {
		name       string
		body       string
		records    []node.Node
		wantStatus int
		wantOps    int
	}{
		{name: "online node", body: `{"service":"config-service","node_id":"worker-node"}`, records: []node.Node{online}, wantStatus: http.StatusAccepted, wantOps: 1},
		{name: "missing node id", body: `{"service":"config-service"}`, records: []node.Node{online}, wantStatus: http.StatusBadRequest},
		{name: "unknown node", body: `{"service":"config-service","node_id":"missing"}`, wantStatus: http.StatusNotFound},
		{name: "draining node", body: `{"service":"config-service","node_id":"worker-node"}`, records: []node.Node{draining}, wantStatus: http.StatusConflict},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nodeRepo := newMemoryNodeRepository(tc.records...)
			opRepo := newMemoryOperationRepository()
			router := newNodeTestServer(t, nodeRepo, opRepo).Handler()

			req := httptest.NewRequest(
				http.MethodPost,
				"/updates",
				strings.NewReader(tc.body),
			)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if len(opRepo.records) != tc.wantOps {
				t.Fatalf("created operations = %d, want %d", len(opRepo.records), tc.wantOps)
			}
			if tc.wantOps == 1 {
				for _, op := range opRepo.records {
					if op.NodeID != "worker-node" {
						t.Fatalf("operation node_id = %q, want worker-node", op.NodeID)
					}
				}
			}
		})
	}
}

func TestNodeRoutes(t *testing.T) {
	record := node.Node{
		ID:             "worker-node",
		InstanceID:     "instance-1",
		Status:         node.StatusOnline,
		Address:        "worker-node:8080",
		Version:        "dev",
		LeaseExpiresAt: time.Now().Add(time.Minute),
	}
	nodeRepo := newMemoryNodeRepository(record)
	router := newNodeTestServer(
		t,
		nodeRepo,
		newMemoryOperationRepository(),
	).Handler()

	tests := []struct {
		method     string
		path       string
		wantStatus int
		wantNode   node.Status
	}{
		{method: http.MethodGet, path: "/nodes", wantStatus: http.StatusOK},
		{method: http.MethodGet, path: "/nodes/worker-node", wantStatus: http.StatusOK},
		{method: http.MethodGet, path: "/nodes/missing", wantStatus: http.StatusNotFound},
		{method: http.MethodPost, path: "/nodes/worker-node/drain", wantStatus: http.StatusOK, wantNode: node.StatusDraining},
		{method: http.MethodPost, path: "/nodes/worker-node/undrain", wantStatus: http.StatusOK, wantNode: node.StatusOnline},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.wantStatus {
			t.Fatalf("%s %s status = %d, want %d; body = %s", tc.method, tc.path, w.Code, tc.wantStatus, w.Body.String())
		}
		if tc.wantNode != "" {
			stored, err := nodeRepo.Get(context.Background(), record.ID)
			if err != nil {
				t.Fatalf("get stored node: %v", err)
			}
			if stored.Status != tc.wantNode {
				t.Fatalf("stored status = %s, want %s", stored.Status, tc.wantNode)
			}
		}
	}
}

func TestProcessNodeMessages(t *testing.T) {
	startedAt := time.Now().Add(-time.Minute).UTC()
	registerData, err := json.Marshal(message.NodeRegister{
		NodeID:       "worker-node",
		InstanceID:   "instance-1",
		Address:      "worker-node:8080",
		Version:      "dev",
		Capabilities: []string{"config-service"},
		StartedAt:    startedAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("marshal registration: %v", err)
	}

	repo := newMemoryNodeRepository()
	if err := processNodeRegister(
		&natslib.Msg{Data: registerData},
		repo,
		20*time.Second,
	); err != nil {
		t.Fatalf("process registration: %v", err)
	}

	stored, err := repo.Get(context.Background(), "worker-node")
	if err != nil {
		t.Fatalf("get registered node: %v", err)
	}
	if stored.Status != node.StatusOnline || stored.InstanceID != "instance-1" {
		t.Fatalf("unexpected registered node: %+v", stored)
	}
	if delta := stored.LeaseExpiresAt.Sub(stored.LastSeenAt) - 20*time.Second; delta > time.Millisecond || delta < -time.Millisecond {
		t.Fatalf("lease duration = %s, want 20s", stored.LeaseExpiresAt.Sub(stored.LastSeenAt))
	}

	heartbeatData, err := json.Marshal(message.NodeHeartbeat{
		NodeID:     "worker-node",
		InstanceID: "instance-1",
	})
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	if err := processNodeHeartbeat(
		&natslib.Msg{Data: heartbeatData},
		repo,
		30*time.Second,
	); err != nil {
		t.Fatalf("process heartbeat: %v", err)
	}

	stored, err = repo.Get(context.Background(), "worker-node")
	if err != nil {
		t.Fatalf("get node after heartbeat: %v", err)
	}
	if delta := stored.LeaseExpiresAt.Sub(stored.LastSeenAt) - 30*time.Second; delta > time.Millisecond || delta < -time.Millisecond {
		t.Fatalf("heartbeat lease duration = %s, want 30s", stored.LeaseExpiresAt.Sub(stored.LastSeenAt))
	}

	goodbyeData, err := json.Marshal(message.NodeGoodbye{
		NodeID:     "worker-node",
		InstanceID: "instance-1",
	})
	if err != nil {
		t.Fatalf("marshal goodbye: %v", err)
	}
	if err := processNodeGoodbye(&natslib.Msg{Data: goodbyeData}, repo); err != nil {
		t.Fatalf("process goodbye: %v", err)
	}

	stored, err = repo.Get(context.Background(), "worker-node")
	if err != nil {
		t.Fatalf("get node after goodbye: %v", err)
	}
	if stored.Status != node.StatusOffline {
		t.Fatalf("status after goodbye = %s, want OFFLINE", stored.Status)
	}
}

func TestDispatchRejectsUnavailableTargetNode(t *testing.T) {
	tests := []struct {
		name    string
		repo    node.Repository
		wantErr error
	}{
		{
			name:    "unknown node",
			repo:    newMemoryNodeRepository(),
			wantErr: node.ErrNotFound,
		},
		{
			name: "draining node",
			repo: newMemoryNodeRepository(node.Node{
				ID:         "worker-node",
				InstanceID: "instance-1",
				Status:     node.StatusDraining,
			}),
			wantErr: errNodeUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := newNodeTestServer(
				t,
				tc.repo,
				newMemoryOperationRepository(),
			)
			err := server.dispatchOperation(
				context.Background(),
				operation.Operation{ID: "op-1", NodeID: "worker-node"},
				operation.FileMetadata{},
			)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("dispatch error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
