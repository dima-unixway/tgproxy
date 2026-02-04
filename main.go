package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tgproxy/api"
	"tgproxy/config"
	"tgproxy/router"
	"tgproxy/telegram"
)

func main() {
	fmt.Println("Starting tgproxy...")

	cfg, err := config.Load()
	if err != nil {
		fmt.Println("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	log := slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level:     cfg.LogLevel,
			AddSource: true,
		}),
	)
	log.Info("Configuration loaded", "port", cfg.HTTPPort)

	router := router.NewRouter(100, log)
	defer router.Close()

	tgClient, err := telegram.NewClient(cfg, router, log)
	if err != nil {
		log.Error("Failed to initialize Telegram client", "error", err)
		os.Exit(1)
	}
	defer tgClient.Close()
	log.Info("Telegram client initialized")

	ctx := context.Background()
	if err := tgClient.Authenticate(ctx); err != nil {
		log.Error("Authentication failed", "error", err)
		os.Exit(1)
	}
	log.Info("Telegram authentication successful")

	go tgClient.StartMessageHandler(ctx)
	log.Info("Message handler started")

	server := api.NewServer(cfg, tgClient, router, log)
	go func() {
		log.Info("Starting HTTP server", "port", cfg.HTTPPort)
		if err := server.Start(); err != nil {
			log.Error("HTTP server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("Server shutdown error", "error", err)
	}

	log.Info("Shutdown complete")
}
