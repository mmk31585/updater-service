package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/mmk31585/updater-service/internal/config"
	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/operation"
	"github.com/nats-io/nats.go"

	_ "github.com/go-sql-driver/mysql"
)

const (
	CommandSubject = "update.command.node-1"
	ResultSubject  = "update.result"
	natsURL        = nats.DefaultURL
	addr           = ":8080"
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
	cnf, err := config.LoadConfig()
	if err != nil {
		logger.Error(
			"failed to initial config",
			"error", err,
		)
		os.Exit(1)
	}
	dsn := buildDSN(cnf.DB)

	if dsn == "" {
		dsn = "updates:updates@tcp(127.0.0.1:3306)/updates?parseTime=true&loc=UTC"
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		logger.Error(
			"failed to open database",
			"error", err,
		)
		os.Exit(1)
	}

	defer db.Close()

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		logger.Error(
			"failed to connect to database",
			"error", err,
		)
		os.Exit(1)
	}

	logger.Info("database connected")

	repo := operation.NewMariaDBRepository(db)
	_, err = nc.Subscribe(
		"update.result",
		func(msg *nats.Msg) {
			handleResult(
				msg,
				repo,
				logger,
			)
		},
	)

	if err != nil {
		logger.Error(
			"failed to subscribe to result topic",
			"error", err,
		)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /updates", func(w http.ResponseWriter, r *http.Request) {
		handleCreateUpdate(w, r, nc, repo, cnf.Node.ID, logger)
	})
	mux.HandleFunc("GET /updates/{id}", func(w http.ResponseWriter, r *http.Request) { handleGetUpdate(w, r, repo) })
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
	repo operation.Repository,
	nodeID string,
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

	now := time.Now().UTC()

	op := operation.Operation{
		ID:        operationID,
		Status:    operation.StatusPending,
		Service:   req.Service,
		NodeID:    nodeID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	ctx := r.Context()

	// 1. Persist operation first.
	if err := repo.Create(ctx, op); err != nil {
		logger.Error(
			"failed to create operation",
			"operation_id", operationID,
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

	// 2. Build command.
	command := message.UpdateCommand{
		OperationID: operationID,
		Service:     req.Service,
	}

	data, err := json.Marshal(command)
	if err != nil {
		logger.Error(
			"failed to encode command",
			"operation_id", operationID,
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

	// 3. Publish command.
	subject := "update.command." + nodeID

	if err := nc.Publish(subject, data); err != nil {
		logger.Error(
			"failed to publish command",
			"operation_id", operationID,
			"error", err,
		)

		// Operation remains PENDING.
		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "failed to dispatch operation",
			},
		)
		return
	}

	if err := nc.Flush(); err != nil {
		logger.Error(
			"failed to flush NATS message",
			"operation_id", operationID,
			"error", err,
		)

		writeJSON(
			w,
			http.StatusServiceUnavailable,
			map[string]string{
				"error": "failed to dispatch operation",
			},
		)
		return
	}

	// 4. Mark operation as DISPATCHED.
	_, err = repo.AdvanceStatus(
		ctx,
		operationID,
		operation.StatusDispatched,
	)
	if err != nil {
		logger.Error(
			"failed to mark operation dispatched",
			"operation_id", operationID,
			"error", err,
		)

		// We don't fail the HTTP request here.
		// Worker may already have moved it forward.
	}

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
func buildDSN(dbConf config.DBConfig) string {
	host := dbConf.Host
	if host == "localhost" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true",
		dbConf.Username,
		dbConf.Password,
		host,
		dbConf.Port,
		dbConf.Name,
	)
}
func handleGetUpdate(
	w http.ResponseWriter,
	r *http.Request,
	repo operation.Repository,
) {
	id := r.PathValue("id")

	op, err := repo.Get(
		r.Context(),
		id,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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
			http.StatusInternalServerError,
			map[string]string{
				"error": "failed to get operation",
			},
		)
		return
	}

	type response struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}

	writeJSON(
		w,
		http.StatusOK,
		response{
			ID:     op.ID,
			Status: string(op.Status),
		},
	)
}
func handleResult(
	msg *nats.Msg,
	repo operation.Repository,
	logger *slog.Logger,
) {
	var result message.UpdateResult

	if err := json.Unmarshal(
		msg.Data,
		&result,
	); err != nil {
		logger.Error(
			"failed to decode update result",
			"error", err,
		)
		return
	}

	logger.Info(
		"received update result",
		"operation_id", result.OperationID,
		"status", result.Status,
	)

	updated, err := repo.AdvanceStatus(
		context.Background(),
		result.OperationID,
		operation.Status(result.Status),
	)
	if err != nil {
		logger.Error(
			"failed to update operation status",
			"operation_id", result.OperationID,
			"status", result.Status,
			"error", err,
		)
		return
	}

	if !updated {
		logger.Info(
			"operation was not advanced",
			"operation_id", result.OperationID,
			"status", result.Status,
		)
		return
	}

	logger.Info(
		"operation status updated",
		"operation_id", result.OperationID,
		"status", result.Status,
	)
}
