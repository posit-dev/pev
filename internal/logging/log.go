// Package logging configures the structured JSON file logger used by pev.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
)

// Init wires logrus to a JSON file under outDir and sets the level.
// It returns the *os.File so callers can close it on shutdown.
func Init(outDir, level string) (*os.File, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create out dir: %w", err)
	}
	ts := time.Now().UTC().Format("20060102T150405")
	path := filepath.Join(outDir, fmt.Sprintf("pev-log-%s.log", ts))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	log.SetFormatter(&log.JSONFormatter{TimestampFormat: time.RFC3339Nano})
	log.SetOutput(io.MultiWriter(f))

	lvl, err := log.ParseLevel(level)
	if err != nil {
		lvl = log.InfoLevel
	}
	log.SetLevel(lvl)
	log.WithField("path", path).Info("pev log initialized")
	return f, nil
}
