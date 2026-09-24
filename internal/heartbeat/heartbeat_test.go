package heartbeat

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/subjects"
	"github.com/mmk31585/updater-service/internal/testutil"
	natslib "github.com/nats-io/nats.go"
)

func newClient(t *testing.T, url string) *nats.Client {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := nats.New(nats.Config{
		URL:             url,
		ConnectTimeout:  2 * time.Second,
		ReconnectPeriod: 100 * time.Millisecond,
		MaxReconnect:    3,
		DrainTimeout:    time.Second,
	}, logger)
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	return client
}

func recv(t *testing.T, sub *natslib.Subscription) []byte {
	t.Helper()
	msg, err := sub.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatalf("receive message: %v", err)
	}
	return msg.Data
}

func TestRegisterPublishesNodeRegister(t *testing.T) {
	client := newClient(t, testutil.StartNATS(t))
	defer client.Close()

	sub, err := client.Connection().SubscribeSync(subjects.NodeRegister)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := client.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	mgr := NewHeartbeatManager(
		client, "node-1", "instance-1", "node-1:8080", "dev",
		[]string{"data-service"}, time.Minute, time.Minute, nil,
	)

	if err := mgr.Register(); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	var got message.NodeRegister
	if err := json.Unmarshal(recv(t, sub), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.NodeID != "node-1" || got.InstanceID != "instance-1" {
		t.Errorf("unexpected identity: %+v", got)
	}
	if got.Address != "node-1:8080" || got.Version != "dev" {
		t.Errorf("unexpected metadata: %+v", got)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0] != "data-service" {
		t.Errorf("Capabilities = %v", got.Capabilities)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.StartedAt); err != nil {
		t.Errorf("StartedAt %q is not RFC3339Nano: %v", got.StartedAt, err)
	}
}

func TestRunRegistersHeartbeatsAndSaysGoodbye(t *testing.T) {
	client := newClient(t, testutil.StartNATS(t))
	defer client.Close()

	regSub, _ := client.Connection().SubscribeSync(subjects.NodeRegister)
	hbSub, _ := client.Connection().SubscribeSync(subjects.NodeHeartbeat)
	byeSub, _ := client.Connection().SubscribeSync(subjects.NodeGoodbye)
	if err := client.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	mgr := NewHeartbeatManager(
		client, "node-1", "instance-1", "node-1:8080", "dev", nil,
		20*time.Millisecond, time.Minute, nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		mgr.Run(ctx)
		close(done)
	}()

	recv(t, regSub)
	recv(t, hbSub)

	cancel()
	recv(t, byeSub)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}
