package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func newLogger(w io.Writer, env string) *slog.Logger {
	var handler slog.Handler

	if strings.EqualFold(env, "production") {
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level:     slog.LevelInfo,
			AddSource: true,
		})
	} else {
		handler = slog.NewTextHandler(w, &slog.HandlerOptions{
			Level:     slog.LevelInfo,
			AddSource: true,
		})
	}

	return slog.New(handler)
}

func NewLogger(appName string, env string, nodeID string) *slog.Logger {
	return newLogger(os.Stdout, env).With(
		"slog", "app",
		"app_name", appName,
		"env", env,
		"node_id", nodeID,
	)
}
