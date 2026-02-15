package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// StatusProvider provides health status information
type StatusProvider interface {
	HealthStatus() map[string]interface{}
}

// Server provides HTTP health check endpoints
type Server struct {
	server   *http.Server
	provider StatusProvider
	logger   zerolog.Logger
}

// New creates a new health check server
func New(port int, provider StatusProvider, logger zerolog.Logger) *Server {
	s := &Server{
		provider: provider,
		logger:   logger.With().Str("component", "health").Logger(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/ready", s.readyHandler)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start starts the health check server
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info().Str("addr", s.server.Addr).Msg("starting health check server")

	errChan := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return fmt.Errorf("health server failed: %w", err)
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	}
}

// healthHandler handles /health endpoint
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	status := s.provider.HealthStatus()

	w.Header().Set("Content-Type", "application/json")

	// Check if both connections are healthy
	mqttOk := status["mqtt_connected"].(bool)
	ircOk := status["irc_connected"].(bool)

	if mqttOk && ircOk {
		w.WriteHeader(http.StatusOK)
		status["status"] = "healthy"
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		status["status"] = "unhealthy"
	}

	if err := json.NewEncoder(w).Encode(status); err != nil {
		s.logger.Error().Err(err).Msg("failed to encode health status")
	}
}

// readyHandler handles /ready endpoint (for Kubernetes readiness probes)
func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	status := s.provider.HealthStatus()

	mqttOk := status["mqtt_connected"].(bool)
	ircOk := status["irc_connected"].(bool)

	if mqttOk && ircOk {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready"))
	}
}

// Shutdown gracefully shuts down the health server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("shutting down health check server")

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("health server shutdown failed: %w", err)
	}

	s.logger.Info().Msg("health check server stopped")
	return nil
}
