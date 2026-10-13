package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"credivect/core/internal/api"
	"credivect/core/internal/auth"
	"credivect/core/internal/bureau"
	"credivect/core/internal/cache"
	"credivect/core/internal/client"
	"credivect/core/internal/config"
	"credivect/core/internal/events"
	"credivect/core/internal/security"
	"credivect/core/internal/storage"
	"credivect/core/internal/workflow"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("credivect-core: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := storage.Open(ctx, cfg.PostgresURL, 12)
	if err != nil {
		return err
	}
	defer repo.Close()

	dragonfly, err := cache.NewDragonflyClient(cfg.DragonflyURL)
	if err != nil {
		return err
	}
	defer dragonfly.Close()

	scoring, err := client.NewScoringClient(cfg.ScoringHost, cfg.ScoringTO)
	if err != nil {
		return err
	}
	defer scoring.Close()

	publisher, err := events.NewPublisher(cfg.KafkaBrokers, cfg.KafkaTopic)
	if err != nil {
		return err
	}
	defer publisher.Close()

	runtime, err := workflow.NewRuntime(cfg.TemporalHost, os.Getenv("TEMPORAL_NAMESPACE"), cfg.TemporalQueue)
	if err != nil {
		return err
	}
	defer runtime.Close()

	hasher, err := security.NewHasher(cfg.SSNKey)
	if err != nil {
		return err
	}
	tokens := auth.NewTokenService(cfg.JWTSecret, cfg.JWTTTL)
	verifiers := []auth.Verifier{tokens}
	if cfg.AuthMode == "oidc" {
		idp, err := auth.NewIDTokenVerifier(auth.OIDCSettings{
			Issuer:    cfg.OIDCIssuer,
			JWKSURL:   cfg.OIDCJWKSURL,
			Audience:  cfg.OIDCAudience,
			RoleClaim: cfg.OIDCRoleClaim,
			KeyTTL:    cfg.OIDCKeyTTL,

			AllowCleartext: cfg.OIDCCleartext,
		})
		if err != nil {
			return err
		}
		verifiers = append(verifiers, idp)
		log.Printf("accepting tokens from %s (audience %s, role claim %s)",
			cfg.OIDCIssuer, cfg.OIDCAudience, cfg.OIDCRoleClaim)
	}

	acts := workflow.NewActivities(repo, scoring, bureau.NewDeterministicProvider(), publisher)
	workerErr := make(chan error, 1)
	go func() { workerErr <- runtime.StartWorker(ctx, acts) }()

	server := api.NewServer(repo, dragonfly, scoring, runtime, publisher, tokens, verifiers, hasher, cfg)
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.CORS(server.Routes()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdown := make(chan struct{})
	go func() {
		<-ctx.Done()
		log.Print("shutting down: draining http connections then stopping the worker")
		drainCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(drainCtx)
		runtime.Close()
		close(shutdown)
	}()

	log.Printf("credivect-core listening on :%s (scoring model %s, queue %s, topic %s)",
		cfg.Port, scoring.ModelVersion(), cfg.TemporalQueue, cfg.KafkaTopic)

	serverErr := make(chan error, 1)
	go func() { serverErr <- httpServer.ListenAndServe() }()

	select {
	case err := <-workerErr:
		// The poller only unblocks on cancellation or a fatal worker error; a
		// clean start must never be read as a reason to exit.
		if err != nil && !errors.Is(err, context.Canceled) {
			stop()
			<-shutdown
			return fmt.Errorf("temporal worker: %w", err)
		}
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			stop()
			<-shutdown
			return err
		}
	case <-ctx.Done():
	}
	stop()
	<-shutdown
	return nil
}
