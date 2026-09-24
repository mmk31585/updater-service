package entry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/node"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/subjects"
	"github.com/mmk31585/updater-service/internal/testutil"
	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"
)

type memoryFileRepository struct {
	records map[string]operation.FileMetadata
}

func newMemoryFileRepository() *memoryFileRepository {
	return &memoryFileRepository{records: make(map[string]operation.FileMetadata)}
}

func (r *memoryFileRepository) Create(_ context.Context, meta operation.FileMetadata) error {
	r.records[meta.OperationID] = meta
	return nil
}

func (r *memoryFileRepository) Get(_ context.Context, id string) (operation.FileMetadata, error) {
	meta, ok := r.records[id]
	if !ok {
		return operation.FileMetadata{}, fmt.Errorf("file metadata %s not found", id)
	}
	return meta, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestNATSClient(t *testing.T, url string) *nats.Client {
	t.Helper()
	client, err := nats.New(nats.Config{
		URL:             url,
		ConnectTimeout:  2 * time.Second,
		ReconnectPeriod: 100 * time.Millisecond,
		MaxReconnect:    3,
		DrainTimeout:    time.Second,
	}, testLogger())
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

func uploadHook(operationID, fileName, filePath string, size int64) handler.HookEvent {
	meta := map[string]string{}
	if operationID != "" {
		meta["operation_id"] = operationID
	}
	if fileName != "" {
		meta["filename"] = fileName
	}

	storage := map[string]string{}
	if filePath != "" {
		storage[filestore.StorageKeyPath] = filePath
	}

	return handler.HookEvent{
		Context: context.Background(),
		Upload: handler.FileInfo{
			ID:       "upload-1",
			Size:     size,
			MetaData: meta,
			Storage:  storage,
		},
	}
}

func TestOnTusdUploadCreate(t *testing.T) {
	t.Run("requires operation id", func(t *testing.T) {
		server := New(Config{Logger: testLogger(), OpRepo: newMemoryOperationRepository()})
		if _, _, err := server.onTusdUploadCreate(uploadHook("", "", "", 0)); err == nil {
			t.Fatal("expected error for missing operation_id, got nil")
		}
	})

	t.Run("requires operation repository", func(t *testing.T) {
		server := New(Config{Logger: testLogger()})
		if _, _, err := server.onTusdUploadCreate(uploadHook("op-1", "", "", 0)); err == nil {
			t.Fatal("expected error for missing repository, got nil")
		}
	})

	t.Run("unknown operation", func(t *testing.T) {
		server := New(Config{Logger: testLogger(), OpRepo: newMemoryOperationRepository()})
		if _, _, err := server.onTusdUploadCreate(uploadHook("missing", "", "", 0)); err == nil {
			t.Fatal("expected error for unknown operation, got nil")
		}
	})

	t.Run("operation not pending", func(t *testing.T) {
		opRepo := newMemoryOperationRepository()
		opRepo.records["op-1"] = operation.Operation{ID: "op-1", Status: operation.StatusSucceeded}
		server := New(Config{Logger: testLogger(), OpRepo: opRepo})
		if _, _, err := server.onTusdUploadCreate(uploadHook("op-1", "", "", 0)); err == nil {
			t.Fatal("expected error for non-pending operation, got nil")
		}
	})

	t.Run("pending operation accepted", func(t *testing.T) {
		opRepo := newMemoryOperationRepository()
		opRepo.records["op-1"] = operation.Operation{ID: "op-1", Status: operation.StatusPending}
		server := New(Config{Logger: testLogger(), OpRepo: opRepo})

		resp, changes, err := server.onTusdUploadCreate(uploadHook("op-1", "", "", 0))
		if err != nil {
			t.Fatalf("onTusdUploadCreate() error = %v", err)
		}
		if resp.StatusCode != 0 {
			t.Errorf("StatusCode = %d, want 0", resp.StatusCode)
		}
		if len(changes.MetaData) != 0 {
			t.Errorf("MetaData changes = %v, want empty", changes.MetaData)
		}
	})
}

func TestOnTusdUploadFinish(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "payload.bin")
	content := []byte("config payload")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	sum := sha256.Sum256(content)
	wantSHA := hex.EncodeToString(sum[:])

	t.Run("missing operation id", func(t *testing.T) {
		server := New(Config{Logger: testLogger()})
		if _, err := server.onTusdUploadFinish(uploadHook("", "f", filePath, int64(len(content)))); err == nil {
			t.Fatal("expected error for missing operation_id, got nil")
		}
	})

	t.Run("missing storage path", func(t *testing.T) {
		server := New(Config{Logger: testLogger()})
		if _, err := server.onTusdUploadFinish(uploadHook("op-1", "f", "", 0)); err == nil {
			t.Fatal("expected error for missing storage path, got nil")
		}
	})

	t.Run("requires file repository", func(t *testing.T) {
		server := New(Config{Logger: testLogger()})
		if _, err := server.onTusdUploadFinish(uploadHook("op-1", "f", filePath, int64(len(content)))); err == nil {
			t.Fatal("expected error for missing file repository, got nil")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		server := New(Config{
			Logger:   testLogger(),
			FileRepo: newMemoryFileRepository(),
		})
		if _, err := server.onTusdUploadFinish(uploadHook("op-1", "f", filepath.Join(dir, "nope"), 0)); err == nil {
			t.Fatal("expected error for missing file, got nil")
		}
	})

	t.Run("success stores metadata and dispatches", func(t *testing.T) {
		client := newTestNATSClient(t, testutil.StartNATS(t))

		sub, err := client.Connection().SubscribeSync(subjects.CommandPrefix + "worker-node")
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		if err := client.Flush(); err != nil {
			t.Fatalf("flush: %v", err)
		}

		opRepo := newMemoryOperationRepository()
		opRepo.records["op-1"] = operation.Operation{
			ID: "op-1", Status: operation.StatusPending, Service: "data-service", NodeID: "worker-node",
		}
		fileRepo := newMemoryFileRepository()
		nodeRepo := newMemoryNodeRepository(node.Node{
			ID: "worker-node", InstanceID: "instance-1", Status: node.StatusOnline,
		})

		server := New(Config{
			Logger:   testLogger(),
			NATS:     client,
			OpRepo:   opRepo,
			FileRepo: fileRepo,
			NodeRepo: nodeRepo,
		})

		resp, err := server.onTusdUploadFinish(uploadHook("op-1", "config.yaml", filePath, int64(len(content))))
		if err != nil {
			t.Fatalf("onTusdUploadFinish() error = %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
		}

		meta, err := fileRepo.Get(context.Background(), "op-1")
		if err != nil {
			t.Fatalf("get file metadata: %v", err)
		}
		if meta.SHA256 != wantSHA {
			t.Errorf("SHA256 = %q, want %q", meta.SHA256, wantSHA)
		}
		if meta.FileSize != int64(len(content)) {
			t.Errorf("FileSize = %d, want %d", meta.FileSize, len(content))
		}
		if meta.FileName != "config.yaml" || meta.Status != "READY" {
			t.Errorf("unexpected metadata: %+v", meta)
		}

		op, _ := opRepo.Get(context.Background(), "op-1")
		if op.Status != operation.StatusDispatched {
			t.Errorf("operation status = %s, want %s", op.Status, operation.StatusDispatched)
		}

		msg, err := sub.NextMsg(2 * time.Second)
		if err != nil {
			t.Fatalf("receive command: %v", err)
		}
		var cmd message.UpdateCommand
		if err := json.Unmarshal(msg.Data, &cmd); err != nil {
			t.Fatalf("unmarshal command: %v", err)
		}
		if cmd.OperationID != "op-1" || cmd.Service != "data-service" {
			t.Errorf("unexpected command: %+v", cmd)
		}
		if cmd.FileSHA256 != wantSHA || cmd.FileSize != int64(len(content)) {
			t.Errorf("unexpected command file fields: %+v", cmd)
		}
		if cmd.NodeInstance != "instance-1" {
			t.Errorf("NodeInstance = %q, want instance-1", cmd.NodeInstance)
		}
	})

	t.Run("defaults file name", func(t *testing.T) {
		client := newTestNATSClient(t, testutil.StartNATS(t))
		opRepo := newMemoryOperationRepository()
		opRepo.records["op-1"] = operation.Operation{ID: "op-1", Status: operation.StatusPending, NodeID: "worker-node"}
		fileRepo := newMemoryFileRepository()
		nodeRepo := newMemoryNodeRepository(node.Node{ID: "worker-node", InstanceID: "i1", Status: node.StatusOnline})

		server := New(Config{
			Logger: testLogger(), NATS: client, OpRepo: opRepo, FileRepo: fileRepo, NodeRepo: nodeRepo,
		})

		if _, err := server.onTusdUploadFinish(uploadHook("op-1", "", filePath, int64(len(content)))); err != nil {
			t.Fatalf("onTusdUploadFinish() error = %v", err)
		}

		meta, _ := fileRepo.Get(context.Background(), "op-1")
		if meta.FileName != "source.bin" {
			t.Errorf("FileName = %q, want source.bin", meta.FileName)
		}
	})
}

func TestCalculateFileSHA256(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(filePath, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := calculateFileSHA256(filePath)
	if err != nil {
		t.Fatalf("calculateFileSHA256() error = %v", err)
	}
	const want = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if got != want {
		t.Errorf("hash = %q, want %q", got, want)
	}

	if _, err := calculateFileSHA256(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
