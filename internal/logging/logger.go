package logging

import (
	"log/slog"
	"os"
	"strings"
)

func NewLogger(appName string, env string, nodeID string) *slog.Logger {
	var handler slog.Handler

	if strings.EqualFold(env, "production") {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     slog.LevelInfo,
			AddSource: true,
		})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level:     slog.LevelInfo,
			AddSource: true,
		})
	}

	return slog.New(handler).With(
		"slog", "app",
		"app_name", appName,
		"env", env,
		"node_id", nodeID,
	)
}
