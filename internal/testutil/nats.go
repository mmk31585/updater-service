package testutil

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

func StartNATS(t *testing.T) string {
	t.Helper()

	srv, err := server.NewServer(&server.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	if err != nil {
		t.Fatalf("start embedded nats: %v", err)
	}

	go srv.Start()

	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("embedded nats did not become ready")
	}

	t.Cleanup(srv.Shutdown)

	return srv.ClientURL()
}
