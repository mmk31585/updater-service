package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/operation"
	natslib "github.com/nats-io/nats.go"
)

const backupTimeLayout = "2006-01-02T15-04-05"

func (s *Server) handleCommand(msg *natslib.Msg) {
	var command message.UpdateCommand

	if err := json.Unmarshal(msg.Data, &command); err != nil {
		s.cfg.Logger.Error("failed to decode command", "error", err)
		return
	}
	if command.NodeInstance != "" && s.cfg.Heartbeat != nil &&
		command.NodeInstance != s.cfg.Heartbeat.InstanceID {
		s.cfg.Logger.Error(
			"rejecting command for stale node instance",
			"operation_id", command.OperationID,
			"node_instance", command.NodeInstance,
		)
		return
	}

	opLogger := s.cfg.Logger.With(
		"operation_id", command.OperationID,
		"service", command.Service,
	)

	opLogger.Info("received update command", "file_size", command.FileSize)

	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.OperationCfg.OperationTimeout)
	defer cancel()

	claimed, op, err := s.cfg.OpRepo.ClaimForExecution(ctx, command.OperationID)
	if err != nil {
		opLogger.Error("failed to claim operation", "error", err)
		return
	}

	if !claimed {
		opLogger.Info(
			"operation already claimed or completed",
			"status", op.Status,
		)
		return
	}

	opLogger.Info("operation claimed")

	if err := s.publishResult(command.OperationID, operation.StatusRunning, ""); err != nil {
		opLogger.Error("failed to publish running", "error", err)
		return
	}

	stagingDir := filepath.Join(
		"./update-worker",
		"staging",
		command.OperationID,
	)

	dest := filepath.Join(
		stagingDir,
		command.FileName,
	)

	downloadCtx, cancelDownload := context.WithTimeout(context.Background(), s.cfg.OperationCfg.OperationTimeout)
	defer cancelDownload()

	err = s.cfg.Downloader.Download(
		downloadCtx,
		command.FileURL,
		dest,
		command.FileSize,
		command.FileSHA256,
	)

	if err != nil {
		opLogger.Error("file download failed", "error", err)
		s.failOperation(command.OperationID, err)
		return
	}

	opLogger.Info("file transferred successfully", "path", dest)

	if err := s.publishResult(command.OperationID, operation.StatusTransferred, ""); err != nil {
		opLogger.Error("failed to publish transferred", "error", err)
		return
	}

	backupPath, err := s.executeUpdate(ctx, command, dest)
	if err != nil {
		opLogger.Error("update failed", "error", err)
		if backupPath != "" {
			rollbackCtx, cancelRollback := context.WithTimeout(
				context.Background(),
				s.cfg.OperationCfg.OperationTimeout,
			)
			defer cancelRollback()
			s.rollbackOperation(rollbackCtx, command, backupPath, err)
			return
		}
		s.handleExecutionFailure(command.OperationID, err)
		return
	}

	if err := s.publishResult(command.OperationID, operation.StatusSucceeded, ""); err != nil {
		opLogger.Error("failed to publish succeeded", "error", err)
		return
	}

	opLogger.Info("operation succeeded")
}

func (s *Server) publishResult(
	operationID string,
	status operation.Status,
	errorMessage string,
) error {
	result := message.UpdateResult{
		OperationID: operationID,
		Status:      string(status),
		Error:       errorMessage,
	}

	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	if err := s.cfg.NATS.Publish(ResultSubject, data); err != nil {
		return err
	}

	return s.cfg.NATS.Flush()
}

func (s *Server) executeUpdate(
	ctx context.Context,
	command message.UpdateCommand,
	stagedPath string,
) (string, error) {
	service, ok := s.cfg.DockerRunner.Service(command.Service)
	if !ok {
		return "", fmt.Errorf(
			"unknown service %q",
			command.Service,
		)
	}

	opLogger := s.cfg.Logger.With("operation_id", command.OperationID)

	backupPath := filepath.Join(
		s.cfg.StorageRoot,
		"backups",
		command.Service,
		fmt.Sprintf(
			"%s.%s",
			filepath.Base(service.ConfigPath),
			time.Now().Format(backupTimeLayout),
		),
	)

	opLogger.Info("starting backup")

	if !s.advanceStatus(ctx, command.OperationID, operation.StatusBackupCreated) {
		return "", fmt.Errorf("failed to advance to BACKUP_CREATED")
	}

	created, err := s.cfg.FileDeployer.Backup(
		service.ConfigPath,
		backupPath,
	)
	if err != nil {
		return "", fmt.Errorf(
			"backup failed: %w",
			err,
		)
	}

	if !created {
		backupPath = ""
		opLogger.Info(
			"no existing config to back up, proceeding",
			"target", service.ConfigPath,
		)
	} else {
		opLogger.Info(
			"backup created",
			"backup", backupPath,
		)
	}

	if !s.advanceStatus(ctx, command.OperationID, operation.StatusApplying) {
		return backupPath, fmt.Errorf("failed to advance to APPLYING")
	}

	if err := s.cfg.FileDeployer.Apply(
		stagedPath,
		service.ConfigPath,
	); err != nil {
		return backupPath, fmt.Errorf(
			"apply failed: %w",
			err,
		)
	}

	opLogger.Info("new configuration applied")

	restartCtx, cancel := context.WithTimeout(
		ctx,
		15*time.Second,
	)
	defer cancel()

	if err := s.cfg.DockerRunner.Restart(
		restartCtx,
		command.Service,
	); err != nil {
		return backupPath, fmt.Errorf(
			"restart failed: %w",
			err,
		)
	}

	if !s.advanceStatus(ctx, command.OperationID, operation.StatusHealthChecking) {
		return backupPath, fmt.Errorf("failed to advance to HEALTH_CHECKING")
	}

	if service.HealthURL == "" {
		return backupPath, fmt.Errorf("health URL not configured for service %q", command.Service)
	}

	if err := s.cfg.HealthChecker.WaitUntilHealthy(
		ctx,
		service.HealthURL,
	); err != nil {
		return backupPath, fmt.Errorf(
			"new version health check failed: %w",
			err,
		)
	}

	return backupPath, nil
}

func (s *Server) rollbackOperation(
	ctx context.Context,
	command message.UpdateCommand,
	backupPath string,
	cause error,
) {
	opLogger := s.cfg.Logger.With("operation_id", command.OperationID)

	opLogger.Warn("starting rollback", "service", command.Service, "cause", cause)

	s.advanceStatus(ctx, command.OperationID, operation.StatusFailed)

	_ = s.publishResult(
		command.OperationID,
		operation.StatusFailed,
		cause.Error(),
	)

	if !s.advanceStatus(ctx, command.OperationID, operation.StatusRollingBack) {
		opLogger.Error("failed to mark ROLLING_BACK")
		return
	}

	_ = s.publishResult(
		command.OperationID,
		operation.StatusRollingBack,
		"",
	)

	service, ok := s.cfg.DockerRunner.Service(command.Service)
	if !ok {
		s.markRollbackFailed(
			command.OperationID,
			"service definition not found",
		)
		return
	}

	if err := s.cfg.FileDeployer.Rollback(
		backupPath,
		service.ConfigPath,
	); err != nil {
		s.markRollbackFailed(
			command.OperationID,
			err.Error(),
		)
		return
	}

	opLogger.Info("previous configuration restored")

	restartCtx, cancel := context.WithTimeout(
		ctx,
		15*time.Second,
	)
	defer cancel()

	if err := s.cfg.DockerRunner.Restart(
		restartCtx,
		command.Service,
	); err != nil {
		s.markRollbackFailed(
			command.OperationID,
			err.Error(),
		)
		return
	}

	s.advanceStatus(ctx, command.OperationID, operation.StatusRollbackHealthChecking)

	_ = s.publishResult(
		command.OperationID,
		operation.StatusRollbackHealthChecking,
		"",
	)

	if service.HealthURL == "" {
		s.markRollbackFailed(
			command.OperationID,
			"health URL not configured",
		)
		return
	}

	if err := s.cfg.HealthChecker.WaitUntilHealthy(
		ctx,
		service.HealthURL,
	); err != nil {
		s.markRollbackFailed(
			command.OperationID,
			err.Error(),
		)
		return
	}

	if !s.advanceStatus(ctx, command.OperationID, operation.StatusRolledBack) {
		opLogger.Error("failed to persist ROLLED_BACK")
		return
	}

	_ = s.publishResult(
		command.OperationID,
		operation.StatusRolledBack,
		"",
	)

	opLogger.Warn("operation rolled back successfully")
}

func (s *Server) markRollbackFailed(
	operationID string,
	reason string,
) {
	opLogger := s.cfg.Logger.With("operation_id", operationID)

	opLogger.Error("rollback failed", "error", reason)

	if !s.advanceStatus(context.Background(), operationID, operation.StatusRollbackFailed) {
		opLogger.Error("failed to persist ROLLBACK_FAILED")
		return
	}

	_ = s.publishResult(
		operationID,
		operation.StatusRollbackFailed,
		reason,
	)
}

func (s *Server) handleExecutionFailure(
	operationID string,
	err error,
) {
	opLogger := s.cfg.Logger.With("operation_id", operationID)

	opLogger.Error("operation execution failed", "error", err)

	failCtx, cancelFail := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFail()

	if !s.advanceStatus(failCtx, operationID, operation.StatusFailed) {
		opLogger.Error("failed to persist FAILED status")
		return
	}

	if publishErr := s.publishResult(
		operationID,
		operation.StatusFailed,
		err.Error(),
	); publishErr != nil {
		opLogger.Error(
			"failed to publish FAILED result",
			"error", publishErr,
		)
	}
}

func (s *Server) failOperation(
	operationID string,
	err error,
) {
	opLogger := s.cfg.Logger.With("operation_id", operationID)

	opLogger.Error("operation failed", "error", err)

	if !s.advanceStatus(context.Background(), operationID, operation.StatusFailed) {
		opLogger.Error("failed to persist FAILED")
		return
	}

	_ = s.publishResult(
		operationID,
		operation.StatusFailed,
		err.Error(),
	)
}
