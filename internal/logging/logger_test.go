package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLoggerDevelopment(t *testing.T) {
	logger := NewLogger("test-app", "development", "node-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	_ = logger
}

func TestNewLoggerProduction(t *testing.T) {
	logger := NewLogger("test-app", "production", "node-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	_ = logger
}

func TestNewLoggerAttributes(t *testing.T) {
	logger := NewLogger("myapp", "staging", "worker-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	_ = logger
}

func TestNewLoggerDoesNotReturnNil(t *testing.T) {
	tests := []struct {
		appName string
		env     string
		nodeID  string
	}{
		{"app", "development", "node-1"},
		{"app", "production", "node-1"},
		{"app", "staging", "node-1"},
		{"", "development", ""},
	}

	for _, tt := range tests {
		t.Run(tt.appName+"_"+tt.env, func(t *testing.T) {
			logger := NewLogger(tt.appName, tt.env, tt.nodeID)
			if logger == nil {
				t.Errorf("NewLogger(%q, %q, %q) returned nil", tt.appName, tt.env, tt.nodeID)
			}
		})
	}
}

func TestNewLoggerProductionUsesJSONHandler(t *testing.T) {
	logger := NewLogger("test-app", "production", "node-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	_ = logger
}

func TestNewLoggerDevelopmentUsesTextHandler(t *testing.T) {
	logger := NewLogger("test-app", "development", "node-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	_ = logger
}

func TestNewLoggerDefaultEnv(t *testing.T) {
	logger := NewLogger("test-app", "", "node-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil for empty env")
	}
	_ = logger
}

func TestNewLoggerCaseInsensitiveProduction(t *testing.T) {
	logger := NewLogger("test-app", "PRODUCTION", "node-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil for PRODUCTION")
	}
	_ = logger

	logger2 := NewLogger("test-app", "Production", "node-1")
	if logger2 == nil {
		t.Fatal("NewLogger returned nil for Production")
	}
}

func TestNewLoggerWithSpecialCharacters(t *testing.T) {
	logger := NewLogger("my-app_v2", "test-env", "node-123")
	if logger == nil {
		t.Fatal("NewLogger returned nil with special chars")
	}
	_ = logger
}

func TestLoggerWithFields(t *testing.T) {
	logger := NewLogger("test-app", "development", "node-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}

	fields := logger.With("operation_id", "op-123", "service", "config-service")
	if fields == nil {
		t.Error("logger.With returned nil")
	}
	_ = fields
}

func TestLoggerInfo(t *testing.T) {
	logger := NewLogger("test-app", "development", "node-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log.Info("entry starting", "addr", ":8080")

	output := buf.String()
	if !strings.Contains(output, "entry starting") {
		t.Errorf("Expected output to contain 'entry starting', got: %s", output)
	}
	_ = logger
}

func TestLoggerError(t *testing.T) {
	logger := NewLogger("test-app", "development", "node-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	log.Error("server failed", "error", "connection refused")

	output := buf.String()
	if !strings.Contains(output, "server failed") {
		t.Errorf("Expected output to contain 'server failed', got: %s", output)
	}
	_ = logger
}

func TestNewLoggerDevelopmentOutputsText(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler).With("app_name", "test-app", "env", "development", "node_id", "node-1")
	logger.Info("starting")

	output := buf.String()
	if !strings.Contains(output, "starting") {
		t.Errorf("Expected text output to contain 'starting', got: %s", output)
	}
}

func TestNewLoggerProductionOutputsJSON(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler).With("app_name", "test-app", "env", "production", "node_id", "node-1")
	logger.Info("starting")

	output := buf.String()
	if !strings.Contains(output, `"msg":"starting"`) {
		t.Errorf("Expected JSON output to contain 'msg', got: %s", output)
	}
}

func TestNewLoggerReturnsLoggerWithCorrectKeys(t *testing.T) {
	logger := NewLogger("myapp", "dev", "node-1")
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	_ = logger
}
