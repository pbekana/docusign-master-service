// Package main is the entry point for the DocuSign Master Service.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/your-org/docusign-master-service/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	_ = cfg
	_ = context.Background()

	slog.Info("DocuSign Master Service starting", "port", cfg.Port, "env", cfg.AppEnv)
}
