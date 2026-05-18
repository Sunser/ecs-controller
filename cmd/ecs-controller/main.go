package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ecs-controller/internal/applog"
	"ecs-controller/internal/config"
	"ecs-controller/internal/monitor"
	"ecs-controller/internal/web"
)

const (
	configPath = "/data/settings.yaml"
	webDir     = "/app/web"
)

func main() {
	configureLogger()
	configureTimezone()

	cfg, err := config.LoadFile(configPath)
	if err != nil {
		applog.Error("config", "load failed", map[string]string{"path": configPath, "error": err.Error()})
		os.Exit(1)
	}
	applog.SetLevel(cfg.Logging.Level)

	state, err := monitor.OpenStateStore(cfg.Server.StatePath)
	if err != nil {
		applog.Error("state", "load failed", map[string]string{"path": cfg.Server.StatePath, "error": err.Error()})
		os.Exit(1)
	}

	service := monitor.NewService(cfg, state, configPath)
	handler := web.NewServer(service, webDir)
	server := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go service.RefreshLoop(ctx)
	go func() {
		applog.Info("server", "listening", map[string]string{"addr": cfg.Server.Listen})
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			applog.Error("server", "http server failed", map[string]string{"error": err.Error()})
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		applog.Error("server", "http shutdown failed", map[string]string{"error": err.Error()})
	}
}

func configureTimezone() {
	tz := os.Getenv("TZ")
	if tz == "" {
		return
	}
	location, err := time.LoadLocation(tz)
	if err != nil {
		applog.Warn("time", "load timezone failed", map[string]string{"tz": tz, "error": err.Error()})
		return
	}
	time.Local = location
}

func configureLogger() {
	applog.SetDefault(applog.New(500, os.Stderr))
}
