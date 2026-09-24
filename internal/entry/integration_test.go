package entry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/node"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/subjects"
	"github.com/mmk31585/updater-service/internal/testutil"
	natslib "github.com/nats-io/nats.go"
)

func tusMetadata(pairs map[string]string) string {
	parts := make([]string, 0, len(pairs))
	for key, value := range pairs {
		parts = append(parts, key+" "+base64.StdEncoding.EncodeToString([]byte(value)))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// TestUploadToDispatchFlow drives the full happy path through the HTTP layer:
// create operation -> tus upload -> NATS dispatch -> worker result handling.
func TestUploadToDispatchFlow(t *testing.T) {
	client := newTestNATSClient(t, testutil.StartNATS(t))

	commandSub, err := client.Connection().SubscribeSync(subjects.CommandPrefix + "worker-node")
	if err != nil {
		t.Fatalf("subscribe command: %v", err)
	}
	if err := client.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	nodeRepo := newMemoryNodeRepository(node.Node{
		ID: "worker-node", InstanceID: "instance-1", Status: node.StatusOnline,
	})
	opRepo := newMemoryOperationRepository()
	fileRepo := newMemoryFileRepository()

	server := New(Config{
		Logger:               testLogger(),
		NATS:                 client,
		OpRepo:               opRepo,
		FileRepo:             fileRepo,
		NodeRepo:             nodeRepo,
		TusdUploadDir:        t.TempDir(),
		TusdMaxSize:          1 << 20,
		NodeHeartbeatTimeout: 20 * time.Second,
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	content := []byte("data-service: version: 2\n")
	sum := sha256.Sum256(content)
	wantSHA := hex.EncodeToString(sum[:])

	// 1. Create the operation.
	createResp := doJSON(t, http.MethodPost, httpServer.URL+"/updates",
		`{"service":"data-service","node_id":"worker-node"}`, nil)
	if createResp.StatusCode != http.StatusAccepted {
		t.Fatalf("create status = %d, want %d; body = %s", createResp.StatusCode, http.StatusAccepted, readBody(t, createResp))
	}
	var created CreateUpdateResponse
	decodeJSON(t, createResp, &created)
	if created.OperationID == "" {
		t.Fatal("empty operation id")
	}

	// 2. Start the tus upload.
	metadata := tusMetadata(map[string]string{
		"operation_id": created.OperationID,
		"filename":     "config.yaml",
	})
	createUploadReq, err := http.NewRequest(http.MethodPost, httpServer.URL+"/uploads", nil)
	if err != nil {
		t.Fatalf("build upload request: %v", err)
	}
	createUploadReq.Header.Set("Tus-Resumable", "1.0.0")
	createUploadReq.Header.Set("Upload-Length", fmt.Sprintf("%d", len(content)))
	createUploadReq.Header.Set("Upload-Metadata", metadata)

	createUploadResp, err := http.DefaultClient.Do(createUploadReq)
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	defer createUploadResp.Body.Close()
	if createUploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("create upload status = %d, want %d; body = %s", createUploadResp.StatusCode, http.StatusCreated, readBody(t, createUploadResp))
	}
	location := createUploadResp.Header.Get("Location")
	if location == "" {
		t.Fatal("missing Location header from tus upload creation")
	}
	if !strings.HasPrefix(location, "http") {
		location = httpServer.URL + location
	}

	// 3. Upload the bytes.
	patchReq, err := http.NewRequest(http.MethodPatch, location, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("build patch request: %v", err)
	}
	patchReq.Header.Set("Tus-Resumable", "1.0.0")
	patchReq.Header.Set("Upload-Offset", "0")
	patchReq.Header.Set("Content-Type", "application/offset+octet-stream")

	patchResp, err := http.DefaultClient.Do(patchReq)
	if err != nil {
		t.Fatalf("patch upload: %v", err)
	}
	defer patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusNoContent {
		t.Fatalf("patch status = %d, want %d; body = %s", patchResp.StatusCode, http.StatusNoContent, readBody(t, patchResp))
	}

	// 4. A dispatch command should have been published to the worker.
	msg, err := commandSub.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatalf("receive command: %v", err)
	}
	var command message.UpdateCommand
	if err := json.Unmarshal(msg.Data, &command); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}
	if command.OperationID != created.OperationID || command.Service != "data-service" {
		t.Errorf("unexpected command: %+v", command)
	}
	if command.NodeInstance != "instance-1" {
		t.Errorf("NodeInstance = %q, want instance-1", command.NodeInstance)
	}
	if command.FileSHA256 != wantSHA || command.FileSize != int64(len(content)) || command.FileName != "config.yaml" {
		t.Errorf("unexpected command file fields: %+v", command)
	}

	// 5. Metadata is persisted with the correct hash.
	meta, err := fileRepo.Get(context.Background(), created.OperationID)
	if err != nil {
		t.Fatalf("get file metadata: %v", err)
	}
	if meta.SHA256 != wantSHA {
		t.Errorf("stored SHA256 = %q, want %q", meta.SHA256, wantSHA)
	}

	// 6. The stored file is served back for worker download (with ranges).
	fileResp := doJSON(t, http.MethodGet, httpServer.URL+"/internal/operations/"+created.OperationID+"/file", "", nil)
	if fileResp.StatusCode != http.StatusOK {
		t.Fatalf("serve file status = %d, want 200", fileResp.StatusCode)
	}
	served := readBody(t, fileResp)
	if served != string(content) {
		t.Errorf("served file = %q, want %q", served, content)
	}

	rangeReq, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/internal/operations/"+created.OperationID+"/file", nil)
	rangeReq.Header.Set("Range", "bytes=0-4")
	rangeResp, err := http.DefaultClient.Do(rangeReq)
	if err != nil {
		t.Fatalf("range request: %v", err)
	}
	defer rangeResp.Body.Close()
	if rangeResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", rangeResp.StatusCode)
	}
	if body := readBody(t, rangeResp); body != string(content[:5]) {
		t.Errorf("range body = %q, want %q", body, content[:5])
	}

	// 7. Simulate the worker reporting success and confirm entry records it.
	resultData, err := json.Marshal(message.UpdateResult{
		OperationID: created.OperationID,
		Status:      string(operation.StatusSucceeded),
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	server.handleResult(&natslib.Msg{Data: resultData})

	op, err := opRepo.Get(context.Background(), created.OperationID)
	if err != nil {
		t.Fatalf("get operation: %v", err)
	}
	if op.Status != operation.StatusSucceeded {
		t.Errorf("final operation status = %s, want %s", op.Status, operation.StatusSucceeded)
	}

	getResp := doJSON(t, http.MethodGet, httpServer.URL+"/updates/"+created.OperationID, "", nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get update status = %d, want 200", getResp.StatusCode)
	}
	var status OperationStatusResponse
	decodeJSON(t, getResp, &status)
	if status.Status != string(operation.StatusSucceeded) {
		t.Errorf("status endpoint returned %q, want SUCCEEDED", status.Status)
	}
}

func TestUploadRejectedForInvalidOperation(t *testing.T) {
	client := newTestNATSClient(t, testutil.StartNATS(t))

	opRepo := newMemoryOperationRepository()
	opRepo.records["op-dispatched"] = operation.Operation{ID: "op-dispatched", Status: operation.StatusDispatched}

	server := New(Config{
		Logger:        testLogger(),
		NATS:          client,
		OpRepo:        opRepo,
		FileRepo:      newMemoryFileRepository(),
		NodeRepo:      newMemoryNodeRepository(),
		TusdUploadDir: t.TempDir(),
		TusdMaxSize:   1 << 20,
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	tests := []struct {
		name        string
		operationID string
		wantStatus  int
	}{
		{name: "unknown operation", operationID: "op-missing", wantStatus: http.StatusNotFound},
		{name: "operation not pending", operationID: "op-dispatched", wantStatus: http.StatusConflict},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/uploads", nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.Header.Set("Tus-Resumable", "1.0.0")
			req.Header.Set("Upload-Length", "5")
			req.Header.Set("Upload-Metadata", tusMetadata(map[string]string{"operation_id": tc.operationID}))

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("create upload: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", resp.StatusCode, tc.wantStatus, readBody(t, resp))
			}
		})
	}
}

func doJSON(t *testing.T, method, url, body string, headers map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, target any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(data)
}
