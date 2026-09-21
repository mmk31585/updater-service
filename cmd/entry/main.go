package main

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/mmk31585/updater-service/internal/message"
	"github.com/nats-io/nats.go"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		logger.Error("failed to connect to NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	command := message.UpdateCommand{
		OperationID: "op-1",
		Service:     "test-service",
	}
	data, err := json.Marshal(command)
	if err != nil {
		logger.Error("failed to encode command", "error", err)
		os.Exit(1)
	}

	subject := "update.command.node-1"

	err = nc.Publish(subject, data)
	if err != nil {
		logger.Error(
			"failed to publish command",
			"error", err,
		)
		os.Exit(1)
	}
	if err := nc.Flush(); err != nil {
		logger.Error(
			"failed to flush message",
			"error", err,
		)
		os.Exit(1)
	}
	logger.Info(
		"update command published",
		"operation_id", command.OperationID,
		"service", command.Service,
	)
}
