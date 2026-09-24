package heartbeat

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/mmk31585/updater-service/internal/message"
	"github.com/mmk31585/updater-service/internal/nats"
	"github.com/mmk31585/updater-service/internal/subjects"
)

type HeartbeatManager struct {
	NATS         *nats.Client
	NodeID       string
	InstanceID   string
	Address      string
	Version      string
	Capabilities []string
	StartedAt    time.Time

	Interval time.Duration
	Lease    time.Duration

	Logger *slog.Logger
}

func NewHeartbeatManager(
	client *nats.Client,
	nodeID string,
	instanceID string,
	address string,
	version string,
	capabilities []string,
	interval time.Duration,
	lease time.Duration,
	logger *slog.Logger,
) *HeartbeatManager {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if lease <= 0 {
		lease = 15 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &HeartbeatManager{
		NATS:         client,
		NodeID:       nodeID,
		InstanceID:   instanceID,
		Address:      address,
		Version:      version,
		Capabilities: capabilities,
		StartedAt:    time.Now().UTC(),
		Interval:     interval,
		Lease:        lease,
		Logger:       logger,
	}
}

func (h *HeartbeatManager) Register() error {
	msg := message.NodeRegister{
		NodeID:       h.NodeID,
		InstanceID:   h.InstanceID,
		Address:      h.Address,
		Version:      h.Version,
		Capabilities: h.Capabilities,
		StartedAt:    h.StartedAt.Format(time.RFC3339Nano),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return h.NATS.PublishAndFlush(
		subjects.NodeRegister,
		data,
	)
}
func (h *HeartbeatManager) Run(
	ctx context.Context,
) {
	if err := h.Register(); err != nil {
		h.Logger.Error(
			"failed to register node",
			"error", err,
		)
	}

	ticker := time.NewTicker(
		h.Interval,
	)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.goodbye()
			return

		case <-ticker.C:
			if err := h.Register(); err != nil {
				h.Logger.Error("failed to refresh node registration", "error", err)
			}
			h.heartbeat()
		}
	}
}

func (h *HeartbeatManager) goodbye() {
	msg := message.NodeGoodbye{
		NodeID:     h.NodeID,
		InstanceID: h.InstanceID,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		h.Logger.Error(
			"failed to encode goodbye",
			"error", err,
		)
		return
	}

	if err := h.NATS.PublishAndFlush(
		subjects.NodeGoodbye,
		data,
	); err != nil {
		h.Logger.Error(
			"failed to publish goodbye",
			"error", err,
		)
	}
}
func (h *HeartbeatManager) heartbeat() {
	msg := message.NodeHeartbeat{
		NodeID:     h.NodeID,
		InstanceID: h.InstanceID,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		h.Logger.Error(
			"failed to encode heartbeat",
			"error", err,
		)
		return
	}

	if err := h.NATS.PublishAndFlush(
		subjects.NodeHeartbeat,
		data,
	); err != nil {
		h.Logger.Error(
			"failed to publish heartbeat",
			"error", err,
		)
	}
}
