package main

import (
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	run(logger)
}

func run(logger *slog.Logger) {
	logger.Info("content-worker.ready", "status", "scaffold")
}
