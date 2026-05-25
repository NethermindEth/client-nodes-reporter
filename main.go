package main

import (
	"client-nodes-reporter/cmd"
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cmd, err := cmd.NewRootCmd()
	if err != nil {
		slog.Error("Failed to create command", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.ExecuteContext(ctx); err != nil {
		slog.Error("Failed to execute command", "error", err)
		os.Exit(1)
	}
}
