package main

import (
	"client-nodes-reporter/cmd"
	"context"
	"log/slog"
	"os"
)

func main() {
	cmd, err := cmd.NewRootCmd()
	if err != nil {
		slog.Error("Failed to create command", "error", err)
		os.Exit(1)
	}

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		slog.Error("Failed to execute command", "error", err)
		os.Exit(1)
	}
}
