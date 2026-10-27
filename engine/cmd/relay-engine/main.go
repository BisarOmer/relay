package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/relay-ide/relay/engine/internal/app"
	"github.com/relay-ide/relay/engine/internal/logging"
)

func main() {
	dataDir := flag.String("data-dir", "", "application data directory")
	migrations := flag.String("migrations", "", "optional path to SQL migrations (default: embedded)")
	flag.Parse()

	log := logging.New(os.Stderr, slog.LevelInfo)
	engine, err := app.New(app.Options{
		DataDir:       *dataDir,
		MigrationsDir: *migrations,
		Log:           log,
	})
	if err != nil {
		log.Error("failed to start engine", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		engine.Shutdown(context.Background())
	}()

	err = engine.Run(ctx)
	// stdin closed (host exited) or signal: release broker connections cleanly.
	engine.Shutdown(context.Background())
	if err != nil && err != context.Canceled {
		log.Error("engine stopped", "err", err)
		os.Exit(1)
	}
}
