// Command mycalendar runs the MyCalendar HTTP service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embed the IANA timezone database so that day, week and month boundaries
	// resolve correctly even on a minimal intranet host with no tzdata package.
	_ "time/tzdata"

	"github.com/lyy1119/MyCalendar/internal/api"
	"github.com/lyy1119/MyCalendar/internal/config"
	"github.com/lyy1119/MyCalendar/internal/service"
	"github.com/lyy1119/MyCalendar/internal/store"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mycalendar:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	// Cancelled on SIGINT or SIGTERM to begin a graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("closing database", "error", err)
		}
	}()

	svc := service.New(db, cfg.Location)
	apiSrv := api.NewServer(svc, logger, version, api.Options{
		DefaultLocation:  cfg.Location,
		DefaultWeekStart: cfg.WeekStart,
		CORSOrigins:      cfg.CORSOrigins,
		AllowAllOrigins:  cfg.AllowAllOrigins(),
		LogRequests:      cfg.LogLevel <= slog.LevelInfo,
	})

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("mycalendar listening",
			"addr", cfg.Addr,
			"database", cfg.DBPath,
			"timezone", cfg.Location.String(),
			"week_start", string(cfg.WeekStart),
			"version", version,
		)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("mycalendar stopped")
	return nil
}
