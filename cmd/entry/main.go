package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/nats-io/nats.go"
)

const (
	subject = "update.command.node-1"
	event   = "update.operation.events"
	natsURL = nats.DefaultURL
	addr    = ":8080"
)

type CreateUpdateRequest struct {
	Service string `json:"service"`
}

type CreateUpdateResponse struct {
	OperationID string `json:"operation_id"`
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	nc, err := nats.Connect(natsURL)
	if err != nil {
		logger.Error("failed to connect to NATS", "error", err)
		os.Exit(1)
	}
	defer nc.Close()
	store := operation.NewStore()
	_, err = nc.Subscribe(
		"update.operation.events",
		func(msg *nats.Msg) {
			var event message.OperationEvent

			if err := json.Unmarshal(
				msg.Data,
				&event,
			); err != nil {
				logger.Error(
					"failed to decode operation event",
					"error", err,
				)
				return
			}

			logger.Info(
				"received operation event",
				"operation_id", event.OperationID,
				"status", event.Status,
			)

			updated := store.UpdateStatus(
				event.OperationID,
				operation.Status(event.Status),
			)

			if !updated {
				logger.Warn(
					"operation not found",
					"operation_id", event.OperationID,
				)
				return
			}

			logger.Info(
				"operation status updated",
				"operation_id", event.OperationID,
				"status", event.Status,
			)
		},
	)

	if err != nil {
		logger.Error(
			"failed to subscribe to operation events",
			"error", err,
		)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /updates", func(w http.ResponseWriter, r *http.Request) {
		handleCreateUpdate(w, r, nc, store, logger)
	})
	mux.HandleFunc("GET /updates/{id}", func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		id := r.PathValue("id")

		op, ok := store.Get(id)
		if !ok {
			writeJSON(
				w,
				http.StatusNotFound,
				map[string]string{
					"error": "operation not found",
				},
			)
			return
		}

		writeJSON(
			w,
			http.StatusOK,
			op,
		)
	})
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("entry started", "addr", addr)

	if err := server.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {
		logger.Error("http server failed", "error", err)
		os.Exit(1)
	}
}

func handleCreateUpdate(
	w http.ResponseWriter,
	r *http.Request,
	nc *nats.Conn,
	store *operation.Store,
	logger *slog.Logger,
) {
	var req CreateUpdateRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "invalid request body",
			},
		)
		return
	}

	if req.Service == "" {
		writeJSON(
			w,
			http.StatusBadRequest,
			map[string]string{
				"error": "service is required",
			},
		)
		return
	}

	operationID, err := newOperationID()
	if err != nil {
		logger.Error(
			"failed to generate operation id",
			"error", err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to create operation",
			},
		)
		return
	}
	command := message.UpdateCommand{
		OperationID: operationID,
		Service:     req.Service}
	op := operation.Operation{
		ID:     operationID,
		Status: operation.StatusPending,
	}
	store.Create(op)
	data, err := json.Marshal(command)
	if err != nil {
		logger.Error(
			"failed to encode command",
			"error", err,
		)

		writeJSON(
			w,
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to create command",
			},
		)
		return
	}

	if err := nc.Publish(subject, data); err != nil {
		logger.Error(
			"failed to publish command",
			"error", err,
		)

		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "failed to publish update",
			},
		)
		return
	}
	store.UpdateStatus(operationID, operation.StatusDispatched)
	// Make sure the publish reached the NATS server.
	if err := nc.Flush(); err != nil {
		logger.Error(
			"failed to flush NATS message",
			"error", err,
		)

		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "failed to publish update",
			},
		)
		return
	}

	logger.Info(
		"update accepted",
		"operation_id", operationID,
		"service", req.Service,
	)

	writeJSON(
		w,
		http.StatusAccepted,
		CreateUpdateResponse{
			OperationID: operationID,
		},
	)
}

func newOperationID() (string, error) {
	b := make([]byte, 2)

	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return "op-" + hex.EncodeToString(b), nil
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	data any,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(data)
}
