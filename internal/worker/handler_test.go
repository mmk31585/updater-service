package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mmk31585/updater-service/internal/config"
	"github.com/mmk31585/updater-service/internal/heartbeat"
	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/mmk31585/updater-service/internal/services"
	"github.com/mmk31585/updater-service/internal/subjects"
	"github.com/mmk31585/updater-service/internal/testutil"
	natslib "github.com/nats-io/nats.go"
)

type memoryOperationRepository struct {
	records    map[string]operation.Operation
	claimErr   error
	advanceErr error
}

func newMemoryOperationRepository(ops ...operation.Operation) *memoryOperationRepository {
	repo := &memoryOperationRepository{records: make(map[string]operation.Operation)}
	for _, op := range ops {
		repo.records[op.ID] = op
	}
	return repo
}

func (r *memoryOperationRepository) Create(_ context.Context, op operation.Operation) error {
	r.records[op.ID] = op
	return nil
}

func (r *memoryOperationRepository) Get(_ context.Context, id string) (operation.Operation, error) {
	op, ok := r.records[id]
	if !ok {
		return operation.Operation{}, operation.ErrNotFound
	}
	return op, nil
}

func (r *memoryOperationRepository) AdvanceStatus(
	_ context.Context,
	id string,
	status operation.Status,
) (bool, error) {
	if r.advanceErr != nil {
		return false, r.advanceErr
	}
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
	if r.claimErr != nil {
		return false, operation.Operation{}, r.claimErr
	}
	op, err := r.Get(ctx, id)
	if err != nil {
		return false, op, err
	}
	return true, op, nil
}

func (r *memoryOperationRepository) ListAll(_ context.Context) ([]operation.Operation, error) {
	ops := make([]operation.Operation, 0, len(r.records))
	for _, op := range r.records {
		ops = append(ops, op)
	}
	return ops, nil
}

type downloadCall struct {
	url  string
	dest string
	size int64
	sha  string
}

type fakeDownloader struct {
	err   error
	calls []downloadCall
}

func (f *fakeDownloader) Download(_ context.Context, url, dest string, size int64, sha string) error {
	f.calls = append(f.calls, downloadCall{url: url, dest: dest, size: size, sha: sha})
	return f.err
}

type fakeDockerRunner struct {
	defs       map[string]services.ServiceDefinition
	restartErr error
	restarts   []string
}

func (f *fakeDockerRunner) Service(name string) (services.ServiceDefinition, bool) {
	def, ok := f.defs[name]
	return def, ok
}

func (f *fakeDockerRunner) Restart(_ context.Context, service string) error {
	f.restarts = append(f.restarts, service)
	return f.restartErr
}

type fakeHealthChecker struct {
	err  error
	urls []string
}

func (f *fakeHealthChecker) WaitUntilHealthy(_ context.Context, url string) error {
	f.urls = append(f.urls, url)
	return f.err
}

type fakeFileDeployer struct {
	backupErr   error
	applyErr    error
	rollbackErr error

	backups   [][2]string
	applies   [][2]string
	rollbacks [][2]string
}

func (f *fakeFileDeployer) Backup(target, backup string) error {
	f.backups = append(f.backups, [2]string{target, backup})
	return f.backupErr
}

func (f *fakeFileDeployer) Apply(staged, target string) error {
	f.applies = append(f.applies, [2]string{staged, target})
	return f.applyErr
}

func (f *fakeFileDeployer) Rollback(backup, target string) error {
	f.rollbacks = append(f.rollbacks, [2]string{backup, target})
	return f.rollbackErr
}

func workerTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testServiceDefinition() services.ServiceDefinition {
	return services.ServiceDefinition{
		Container:  "data-service",
		ConfigPath: "/srv/data-service/config.yaml",
		HealthURL:  "http://data-service:8080/health",
	}
}

func newFakes() (*fakeDownloader, *fakeDockerRunner, *fakeHealthChecker, *fakeFileDeployer) {
	return &fakeDownloader{},
		&fakeDockerRunner{defs: map[string]services.ServiceDefinition{"data-service": testServiceDefinition()}},
		&fakeHealthChecker{},
		&fakeFileDeployer{}
}

func TestExecuteUpdate(t *testing.T) {
	command := message.UpdateCommand{
		OperationID: "op-1",
		Service:     "data-service",
		FileName:    "config.yaml",
	}

	t.Run("success", func(t *testing.T) {
		downloader, dockerRunner, checker, deployer := newFakes()
		opRepo := newMemoryOperationRepository(operation.Operation{ID: "op-1", Status: operation.StatusTransferred})
		server := New(Config{
			Logger:        workerTestLogger(),
			OpRepo:        opRepo,
			Downloader:    downloader,
			DockerRunner:  dockerRunner,
			HealthChecker: checker,
			FileDeployer:  deployer,
		})

		backupPath, err := server.executeUpdate(context.Background(), command, "/staging/config.yaml")
		if err != nil {
			t.Fatalf("executeUpdate() error = %v", err)
		}
		if backupPath == "" {
			t.Error("expected non-empty backup path")
		}
		if len(deployer.backups) != 1 || len(deployer.applies) != 1 {
			t.Errorf("expected one backup and one apply, got %d/%d", len(deployer.backups), len(deployer.applies))
		}
		if len(dockerRunner.restarts) != 1 || dockerRunner.restarts[0] != "data-service" {
			t.Errorf("restarts = %v, want [data-service]", dockerRunner.restarts)
		}
		if len(checker.urls) != 1 || checker.urls[0] != testServiceDefinition().HealthURL {
			t.Errorf("health urls = %v", checker.urls)
		}

		op, _ := opRepo.Get(context.Background(), "op-1")
		if op.Status != operation.StatusHealthChecking {
			t.Errorf("final status = %s, want %s", op.Status, operation.StatusHealthChecking)
		}
	})

	t.Run("unknown service", func(t *testing.T) {
		downloader, dockerRunner, checker, deployer := newFakes()
		opRepo := newMemoryOperationRepository(operation.Operation{ID: "op-1"})
		server := New(Config{
			Logger: workerTestLogger(), OpRepo: opRepo,
			Downloader: downloader, DockerRunner: dockerRunner,
			HealthChecker: checker, FileDeployer: deployer,
		})

		cmd := command
		cmd.Service = "missing-service"
		backupPath, err := server.executeUpdate(context.Background(), cmd, "/staging/config.yaml")
		if err == nil {
			t.Fatal("expected error for unknown service, got nil")
		}
		if backupPath != "" {
			t.Errorf("backupPath = %q, want empty", backupPath)
		}
	})

	t.Run("backup failure", func(t *testing.T) {
		downloader, dockerRunner, checker, deployer := newFakes()
		deployer.backupErr = fmt.Errorf("disk full")
		opRepo := newMemoryOperationRepository(operation.Operation{ID: "op-1"})
		server := New(Config{
			Logger: workerTestLogger(), OpRepo: opRepo,
			Downloader: downloader, DockerRunner: dockerRunner,
			HealthChecker: checker, FileDeployer: deployer,
		})

		backupPath, err := server.executeUpdate(context.Background(), command, "/staging/config.yaml")
		if err == nil {
			t.Fatal("expected backup error, got nil")
		}
		if backupPath != "" {
			t.Errorf("backupPath = %q, want empty", backupPath)
		}
	})

	t.Run("apply failure returns backup path", func(t *testing.T) {
		downloader, dockerRunner, checker, deployer := newFakes()
		deployer.applyErr = fmt.Errorf("permission denied")
		opRepo := newMemoryOperationRepository(operation.Operation{ID: "op-1"})
		server := New(Config{
			Logger: workerTestLogger(), OpRepo: opRepo,
			Downloader: downloader, DockerRunner: dockerRunner,
			HealthChecker: checker, FileDeployer: deployer,
		})

		backupPath, err := server.executeUpdate(context.Background(), command, "/staging/config.yaml")
		if err == nil {
			t.Fatal("expected apply error, got nil")
		}
		if backupPath == "" {
			t.Error("expected non-empty backup path for rollback")
		}
	})

	t.Run("health failure returns backup path", func(t *testing.T) {
		downloader, dockerRunner, checker, deployer := newFakes()
		checker.err = fmt.Errorf("unhealthy")
		opRepo := newMemoryOperationRepository(operation.Operation{ID: "op-1"})
		server := New(Config{
			Logger: workerTestLogger(), OpRepo: opRepo,
			Downloader: downloader, DockerRunner: dockerRunner,
			HealthChecker: checker, FileDeployer: deployer,
		})

		backupPath, err := server.executeUpdate(context.Background(), command, "/staging/config.yaml")
		if err == nil {
			t.Fatal("expected health error, got nil")
		}
		if backupPath == "" {
			t.Error("expected non-empty backup path for rollback")
		}
	})
}

func collectResults(t *testing.T, sub *natslib.Subscription) []message.UpdateResult {
	t.Helper()
	var results []message.UpdateResult
	for {
		msg, err := sub.NextMsg(300 * time.Millisecond)
		if err != nil {
			return results
		}
		var result message.UpdateResult
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		results = append(results, result)
	}
}

func newWorkerTestNATS(t *testing.T) (*nats.Client, *natslib.Subscription) {
	t.Helper()
	client, err := nats.New(nats.Config{
		URL:             testutil.StartNATS(t),
		ConnectTimeout:  2 * time.Second,
		ReconnectPeriod: 100 * time.Millisecond,
		MaxReconnect:    3,
		DrainTimeout:    time.Second,
	}, workerTestLogger())
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	t.Cleanup(client.Close)

	sub, err := client.Connection().SubscribeSync(subjects.Result)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := client.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return client, sub
}

func statusSequence(results []message.UpdateResult) []string {
	statuses := make([]string, 0, len(results))
	for _, result := range results {
		statuses = append(statuses, result.Status)
	}
	return statuses
}

func TestHandleCommand(t *testing.T) {
	command := message.UpdateCommand{
		OperationID: "op-1",
		Service:     "data-service",
		FileName:    "config.yaml",
		FileURL:     "http://entry/internal/operations/op-1/file",
		FileSize:    42,
		FileSHA256:  "deadbeef",
	}
	commandData, err := json.Marshal(command)
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}

	newServer := func(
		client *nats.Client,
		opRepo *memoryOperationRepository,
		downloader *fakeDownloader,
		dockerRunner *fakeDockerRunner,
		checker *fakeHealthChecker,
		deployer *fakeFileDeployer,
	) *Server {
		return New(Config{
			Logger:        workerTestLogger(),
			NATS:          client,
			OpRepo:        opRepo,
			Downloader:    downloader,
			DockerRunner:  dockerRunner,
			HealthChecker: checker,
			FileDeployer:  deployer,
			OperationCfg:  config.OperationConfig{OperationTimeout: 5 * time.Second},
		})
	}

	t.Run("success", func(t *testing.T) {
		client, sub := newWorkerTestNATS(t)
		downloader, dockerRunner, checker, deployer := newFakes()
		opRepo := newMemoryOperationRepository(operation.Operation{ID: "op-1", Status: operation.StatusDispatched})

		newServer(client, opRepo, downloader, dockerRunner, checker, deployer).
			handleCommand(&natslib.Msg{Data: commandData})

		results := collectResults(t, sub)
		want := []string{"RUNNING", "TRANSFERRED", "SUCCEEDED"}
		if got := statusSequence(results); !equalStrings(got, want) {
			t.Fatalf("statuses = %v, want %v", got, want)
		}
		if len(downloader.calls) != 1 || downloader.calls[0].sha != "deadbeef" {
			t.Errorf("download calls = %+v", downloader.calls)
		}

		op, _ := opRepo.Get(context.Background(), "op-1")
		if op.Status != operation.StatusHealthChecking {
			t.Errorf("final op status = %s, want %s", op.Status, operation.StatusHealthChecking)
		}
	})

	t.Run("download failure", func(t *testing.T) {
		client, sub := newWorkerTestNATS(t)
		downloader, dockerRunner, checker, deployer := newFakes()
		downloader.err = fmt.Errorf("network down")
		opRepo := newMemoryOperationRepository(operation.Operation{ID: "op-1", Status: operation.StatusDispatched})

		newServer(client, opRepo, downloader, dockerRunner, checker, deployer).
			handleCommand(&natslib.Msg{Data: commandData})

		results := collectResults(t, sub)
		want := []string{"RUNNING", "FAILED"}
		if got := statusSequence(results); !equalStrings(got, want) {
			t.Fatalf("statuses = %v, want %v", got, want)
		}
		if len(results) > 1 && results[1].Error == "" {
			t.Error("expected FAILED result to carry an error message")
		}
	})

	t.Run("apply failure triggers rollback", func(t *testing.T) {
		client, sub := newWorkerTestNATS(t)
		downloader, dockerRunner, checker, deployer := newFakes()
		deployer.applyErr = fmt.Errorf("bad config")
		opRepo := newMemoryOperationRepository(operation.Operation{ID: "op-1", Status: operation.StatusDispatched})

		newServer(client, opRepo, downloader, dockerRunner, checker, deployer).
			handleCommand(&natslib.Msg{Data: commandData})

		results := collectResults(t, sub)
		want := []string{"RUNNING", "TRANSFERRED", "FAILED", "ROLLING_BACK", "ROLLBACK_HEALTH_CHECKING", "ROLLED_BACK"}
		if got := statusSequence(results); !equalStrings(got, want) {
			t.Fatalf("statuses = %v, want %v", got, want)
		}
		if len(deployer.rollbacks) != 1 {
			t.Errorf("rollbacks = %v, want 1", deployer.rollbacks)
		}

		op, _ := opRepo.Get(context.Background(), "op-1")
		if op.Status != operation.StatusRolledBack {
			t.Errorf("final op status = %s, want %s", op.Status, operation.StatusRolledBack)
		}
	})

	t.Run("rollback failure marks rollback failed", func(t *testing.T) {
		client, sub := newWorkerTestNATS(t)
		downloader, dockerRunner, checker, deployer := newFakes()
		deployer.applyErr = fmt.Errorf("bad config")
		deployer.rollbackErr = fmt.Errorf("restore failed")
		opRepo := newMemoryOperationRepository(operation.Operation{ID: "op-1", Status: operation.StatusDispatched})

		newServer(client, opRepo, downloader, dockerRunner, checker, deployer).
			handleCommand(&natslib.Msg{Data: commandData})

		results := collectResults(t, sub)
		if got := statusSequence(results); len(got) == 0 || got[len(got)-1] != "ROLLBACK_FAILED" {
			t.Fatalf("statuses = %v, want last ROLLBACK_FAILED", got)
		}

		op, _ := opRepo.Get(context.Background(), "op-1")
		if op.Status != operation.StatusRollbackFailed {
			t.Errorf("final op status = %s, want %s", op.Status, operation.StatusRollbackFailed)
		}
	})

	t.Run("skips command for stale node instance", func(t *testing.T) {
		client, sub := newWorkerTestNATS(t)
		downloader, dockerRunner, checker, deployer := newFakes()
		opRepo := newMemoryOperationRepository(operation.Operation{ID: "op-1", Status: operation.StatusDispatched})

		staleCommand := command
		staleCommand.NodeInstance = "other-instance"
		data, _ := json.Marshal(staleCommand)

		server := newServer(client, opRepo, downloader, dockerRunner, checker, deployer)
		server.cfg.Heartbeat = &heartbeat.HeartbeatManager{InstanceID: "this-instance"}
		server.handleCommand(&natslib.Msg{Data: data})

		if results := collectResults(t, sub); len(results) != 0 {
			t.Fatalf("expected no results, got %v", statusSequence(results))
		}
		if len(downloader.calls) != 0 {
			t.Error("expected downloader not to be called")
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
