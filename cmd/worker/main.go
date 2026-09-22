package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/mmk31585/updater-service/internal/message"
	"github.com/nats-io/nats.go"
)

const (
	subject = "update.command.node-1"
	natsURL = nats.DefaultURL
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	nc, err := nats.Connect(natsURL)
	if err != nil {
		logger.Error("failed to connect to NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	_, err = nc.Subscribe(subject, func(msg *nats.Msg) {
		var command message.UpdateCommand

		if err := json.Unmarshal(msg.Data, &command); err != nil {
			logger.Error(
				"failed to decode message",
				"error", err,
			)
			return
		}

		logger.Info(
			"received update command",
			"operation_id", command.OperationID,
			"service", command.Service,
		)
		if err := publishOperationEvent(
			nc,
			command.OperationID,
			"RUNNING",
		); err != nil {
			logger.Error(
				"failed to publish RUNNING event",
				"operation_id", command.OperationID,
				"error", err,
			)
			return
		}

		// 2. Fake work.
		time.Sleep(20 * time.Second)

		// 3. Tell Entry that work has finished.
		if err := publishOperationEvent(
			nc,
			command.OperationID,
			"SUCCEEDED",
		); err != nil {
			logger.Error(
				"failed to publish SUCCEEDED event",
				"operation_id", command.OperationID,
				"error", err,
			)
			return
		}

		logger.Info(
			"operation succeeded",
			"operation_id", command.OperationID,
		)
	})
	if err != nil {
		logger.Error("failed to subscribe", "error", err)
		os.Exit(1)
	}

	logger.Info(
		"worker started",
		"subject", subject,
	)
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
	)
	defer stop()

	<-ctx.Done()

	logger.Info("worker shutting down")
}
func publishOperationEvent(
	nc *nats.Conn,
	operationID string,
	status message.OperationStatus,
) error {
	event := message.OperationEvent{
		OperationID: operationID,
		Status:      status,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return nc.Publish(
		"update.operation.events",
		data,
	)
}
