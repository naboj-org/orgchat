package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/redis/go-redis/v9"
)

var (
	errCursor   = errors.New("invalid cursor")
	errConflict = errors.New("idempotency conflict")
)

type Server struct {
	store        *Store
	hub          *Hub
	secret       []byte
	issuer       string
	audience     string
	secureCookie bool
	origins      map[string]bool
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}

func main() {
	if err := run(); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return errors.New("JWT_SECRET must contain at least 32 bytes")
	}

	cookieSecure := env("COOKIE_SECURE", "true")
	if cookieSecure != "true" && cookieSecure != "false" {
		return errors.New("COOKIE_SECURE must be true or false")
	}

	options, err := redis.ParseURL(env("REDIS_URL", "redis://127.0.0.1:6379/0"))
	if err != nil {
		return err
	}

	db := redis.NewClient(options)
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = db.Ping(pingCtx).Err()
	cancel()
	if err != nil {
		return err
	}

	s := &Server{
		store:        &Store{db: db},
		hub:          newHub(),
		secret:       []byte(secret),
		issuer:       os.Getenv("JWT_ISSUER"),
		audience:     os.Getenv("JWT_AUDIENCE"),
		secureCookie: cookieSecure == "true",
		origins:      make(map[string]bool),
	}

	for _, origin := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			s.origins[origin] = true
		}
	}

	e := s.router()
	e.Server.ReadHeaderTimeout = 5 * time.Second
	e.Server.ReadTimeout = 15 * time.Second
	e.Server.WriteTimeout = 15 * time.Second
	e.Server.IdleTimeout = 60 * time.Second
	e.Server.MaxHeaderBytes = 32 << 10

	address := net.JoinHostPort(env("HOST", "127.0.0.1"), env("PORT", "3001"))

	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)

		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			if err := s.store.cleanup(cleanupCtx); err != nil && ctx.Err() == nil {
				log.Error("message cleanup failed", "error", err)
			}
			cancel()

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()

		s.hub.closeAll()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := e.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTP shutdown failed", "error", err)
		}
	}()

	log.Info("orgchat listening", "address", address)
	err = e.Start(address)
	stop()
	<-shutdownDone
	<-cleanupDone

	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}
