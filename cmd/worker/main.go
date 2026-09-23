package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"

	"github.com/mmk31585/updater-service/internal/docker"
	"github.com/mmk31585/updater-service/internal/message"
	"github.com/nats-io/nats.go"
)

const (
	CommandSubject = "update.command.node-1"
	ResultSubject  = "update.result"
	natsURL        = nats.DefaultURL
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	nc, err := nats.Connect(natsURL)
	if err != nil {
		logger.Error("failed to connect to NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()
	dockerRunner := docker.NewRunner()
	_, err = nc.Subscribe(CommandSubject, func(msg *nats.Msg) {
		handleCommand(nc, msg, dockerRunner, logger)
	})
	if err != nil {
		logger.Error("failed to subscribe", "error", err)
		os.Exit(1)
	}

	logger.Info(
		"worker started",
		"subject", CommandSubject,
	)
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
	)
	defer stop()

	<-ctx.Done()

	logger.Info("worker shutting down")
}

func handleCommand(
	nc *nats.Conn,
	msg *nats.Msg,
	runner *docker.Runner,
	logger *slog.Logger,
) {
	var command message.UpdateCommand

	if err := json.Unmarshal(
		msg.Data,
		&command,
	); err != nil {
		logger.Error(
			"failed to decode update command",
			"error", err,
		)
		return
	}

	logger.Info(
		"received update command",
		"operation_id", command.OperationID,
		"service", command.Service,
	)

	// 1. Tell Entry that the work has started.
	if err := publishResult(
		nc,
		command.OperationID,
		"RUNNING",
	); err != nil {
		logger.Error(
			"failed to publish RUNNING result",
			"operation_id", command.OperationID,
			"error", err,
		)
		return
	}
	//2. docker restart service
	if err := runner.Restart(
		context.Background(),
		command.Service,
	); err != nil {

		logger.Error(
			"docker restart failed",
			"operation_id", command.OperationID,
			"service", command.Service,
			"error", err,
		)
		if publishErr := publishResult(
			nc,
			command.OperationID,
			"FAILED",
		); publishErr != nil {
			logger.Error(
				"failed to publish FAILED result",
				"operation_id", command.OperationID,
				"error", publishErr,
			)
		}

		return
	}
	// 3. Tell Entry that the work succeeded.
	if err := publishResult(
		nc,
		command.OperationID,
		"SUCCEEDED",
	); err != nil {
		logger.Error(
			"failed to publish SUCCEEDED result",
			"operation_id", command.OperationID,
			"error", err,
		)
		return
	}

	logger.Info(
		"operation completed successfully",
		"operation_id", command.OperationID,
	)
}
func publishResult(
	nc *nats.Conn,
	operationID string,
	status string,
) error {
	result := message.UpdateResult{
		OperationID: operationID,
		Status:      status,
	}

	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	if err := nc.Publish(ResultSubject, data); err != nil {
		return err
	}

	return nc.Flush()
}
