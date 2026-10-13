package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"credivect/core/internal/config"
	"credivect/core/internal/ingest"
	"credivect/core/internal/storage"
)

// ingest is the reader side of the application event stream. It consumes
// credivect.application.events under a consumer group and lands each message in
// application_events, which the GET /events endpoint serves back. Without it the
// topic has producers and no destination.
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("ingest: configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := storage.Open(ctx, cfg.PostgresURL, 4)
	if err != nil {
		slog.Error("ingest: postgres", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	consumer, err := ingest.New(cfg.KafkaBrokers, cfg.KafkaTopic, "credivect-ingest", repo)
	if err != nil {
		slog.Error("ingest: kafka", "error", err)
		os.Exit(1)
	}

	port := getenv("INGEST_PORT", "8090")
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           routes(repo),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := consumer.Run(ctx); err != nil {
			slog.Error("consumer stopped", "error", err)
		}
	}()
	go func() {
		slog.Info("ingest listening", "port", port, "topic", cfg.KafkaTopic)
		if err := server.ListenAndServe(); err != nil && ctx.Err() == nil {
			slog.Error("http server", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func routes(repo *storage.Repository) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		counts, err := repo.CountStreamEventsByType(r.Context())
		status := http.StatusOK
		body := map[string]any{"service": "credivect-ingest", "events": counts}
		if err != nil {
			status = http.StatusServiceUnavailable
			body["error"] = err.Error()
			body["events"] = map[string]int64{}
		}
		writeJSON(w, status, body)
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		limit := queryInt(r, "limit", 50, 1, 500)
		appID := r.URL.Query().Get("application_id")
		events, err := repo.ListStreamEvents(r.Context(), appID, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events, "count": len(events), "limit": limit})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func queryInt(r *http.Request, key string, fallback, min, max int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return fallback
		}
		n = n*10 + int(c-'0')
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
