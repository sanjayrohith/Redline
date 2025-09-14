// Command gateway runs the Redline API gateway.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sanjayrohith/redline/internal/app"
	"github.com/sanjayrohith/redline/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(configPath())
	if err != nil {
		return err
	}

	a, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer a.Close()

	return a.Run(ctx)
}

func configPath() string {
	if path := os.Getenv("REDLINE_CONFIG_FILE"); path != "" {
		return path
	}
	return "config.yaml"
}
