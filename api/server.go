package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"tgproxy/config"
	"tgproxy/router"
	"tgproxy/telegram"

	"github.com/gorilla/mux"
)

type Server struct {
	cfg      *config.Config
	tg       *telegram.Client
	router   *router.Router
	log      *slog.Logger
	server   *http.Server
	handlers *Handlers
	r        *mux.Router
}

func NewServer(cfg *config.Config, tg *telegram.Client, router *router.Router, log *slog.Logger) *Server {
	handlers := NewHandlers(tg, log, router)

	r := mux.NewRouter()
	r.SkipClean(true)

	var handler http.Handler = r

	r.HandleFunc("/send", handlers.SendMessage)
	r.HandleFunc("/peers", handlers.GetPeers)
	r.HandleFunc("/peers/{query}", handlers.GetPeers)
	r.HandleFunc("/messages", handlers.MessagesHandler)
	r.HandleFunc("/messages/{chats}", handlers.MessagesHandler)
	r.HandleFunc("/subscribe", handlers.SubscribeHandler)
	r.HandleFunc("/subscribe/{chats}", handlers.SubscribeHandler)
	r.HandleFunc("/attachment/{url:.*}", handlers.GetAttachment)
	r.HandleFunc("/health", handlers.HealthCheck)

	log.Info("Routes registered",
		"endpoints", []string{"/send", "/contacts", "/health"})

	return &Server{
		cfg:      cfg,
		tg:       tg,
		router:   router,
		log:      log,
		handlers: handlers,
		r:        r,
		server: &http.Server{
			Addr:         ":" + cfg.HTTPPort,
			Handler:      handler,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}
}

func (s *Server) Start() error {
	s.log.Info("HTTP server starting",
		"addr", s.server.Addr,
		"read_timeout", s.server.ReadTimeout,
		"write_timeout", s.server.WriteTimeout)

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		s.log.Error("HTTP server error", "error", err)
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("HTTP server shutting down...")

	if err := s.server.Shutdown(ctx); err != nil {
		s.log.Error("Server shutdown error", "error", err)
		return err
	}

	s.log.Info("HTTP server shutdown complete")
	return nil
}
