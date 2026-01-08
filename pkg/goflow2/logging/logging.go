package logging

import (
	"fmt"
	"log/slog"
	"os"
)

// NewLogger constructs a slog logger from level/format inputs.
func NewLogger(level, format string) (*slog.Logger, error) {
	var loglevel slog.Level
	if err := loglevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("parse log level %q: %w", level, err)
	}

	opts := slog.HandlerOptions{
		Level: loglevel,
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &opts))
	if format == "json" {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, &opts))
	}

	return logger, nil
}
