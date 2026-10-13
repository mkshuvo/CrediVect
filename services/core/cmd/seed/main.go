package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"credivect/core/internal/auth"
	"credivect/core/internal/cache"
	"credivect/core/internal/client"
	"credivect/core/internal/config"
	"credivect/core/internal/domain"
	"credivect/core/internal/events"
	"credivect/core/internal/security"
	"credivect/core/internal/storage"
	"credivect/core/internal/workflow"

	"golang.org/x/crypto/bcrypt"
)

// seed creates a realistic underwriting book: named users, the registered model
// version, and applications spread across income bands so the queue, governance
// view and adverse action notices all have genuine scored content. Every
// application runs the real saga, so nothing in the dashboard is fabricated.
func main() {
	count := flag.Int("applications", 24, "number of applications to originate")
	clean := flag.Bool("clean", false, "truncate application rows before seeding")
	flag.Parse()

	// Deterministic generator: the same seed must produce the same book.
	// nozero: rng is only used for population attributes, not for key material.
	rng := rand.New(rand.NewSource(20260903))

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := storage.Open(ctx, cfg.PostgresURL, 4)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	defer repo.Close()

	dragonfly, err := cache.NewDragonflyClient(cfg.DragonflyURL)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	defer dragonfly.Close()

	scoring, err := client.NewScoringClient(cfg.ScoringHost, cfg.ScoringTO)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	defer scoring.Close()

	publisher, err := events.NewPublisher(cfg.KafkaBrokers, cfg.KafkaTopic)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	defer publisher.Close()

	runtime, err := workflow.NewRuntime(cfg.TemporalHost, os.Getenv("TEMPORAL_NAMESPACE"), cfg.TemporalQueue)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	defer runtime.Close()

	hasher, err := security.NewHasher(cfg.SSNKey)
	if err != nil {
		log.Fatalf("seed: %v", err)
	}

	if err := seedUsers(ctx, repo); err != nil {
		log.Fatalf("seed users: %v", err)
	}
	mv, err := registerModelVersion(ctx, repo, scoring)
	if err != nil {
		log.Fatalf("seed model version: %v", err)
	}
	if *clean {
		if err := truncate(ctx, repo); err != nil {
			log.Fatalf("seed clean: %v", err)
		}
		log.Print("cleared previous application book")
	}

	submitted := 0
	tokens := map[string]string{}
	for i := 0; i < *count; i++ {
		app, statusToken, err := originate(ctx, repo, hasher, runtime, rng, i)
		if err != nil {
			log.Printf("seed: application %d failed: %v", i, err)
			continue
		}
		tokens[app.ID] = statusToken
		submitted++
		log.Printf("originated %s amount=%d cents term=%d workflow=%s", app.ID, app.RequestedAmountCents, app.LoanTermMonths, app.WorkflowID)
	}

	waitForBook(ctx, repo, mv.ID, time.Duration(*count)*300*time.Millisecond+45*time.Second)

	// Two of the parked reviews get a human decision so the queue shows both
	// automated and manual outcomes.
	signalOpenReviews(ctx, repo, runtime)

	printBorrowerLinks(ctx, repo, tokens)

	log.Printf("seed complete: %d applications originated against model %s", submitted, mv.Version)
}

// printBorrowerLinks hands over one working borrower link per decided outcome.
// The status page and the adverse action notice are only reachable with the
// capability token minted at origination, so without this the seeded book could
// be walked from the underwriter's side alone.
func printBorrowerLinks(ctx context.Context, repo *storage.Repository, tokens map[string]string) {
	for _, status := range []string{"DECLINED", "APPROVED"} {
		apps, err := repo.ListApplications(ctx, status, "", 25, 0)
		if err != nil {
			log.Printf("seed: could not list %s applications: %v", status, err)
			return
		}
		for _, app := range apps {
			if tok, ok := tokens[app.ID]; ok {
				log.Printf("borrower link (%s): /status/%s#t=%s", status, app.ID, tok)
				break
			}
		}
	}
}

func seedUsers(ctx context.Context, repo *storage.Repository) error {
	accounts := []struct {
		id, email, name, role, password string
	}{
		{"01918a2b-4c5d-7000-8000-0000000000a1", "sarah.jenkins@credivect.example", "Sarah Jenkins", auth.RoleUnderwriter, "credivect-underwriter"},
		{"01918a2b-4c5d-7000-8000-0000000000a2", "risk.admin@credivect.example", "Risk Admin", auth.RoleAdmin, "credivect-admin"},
		{"01918a2b-4c5d-7000-8000-0000000000a3", "alex.chen@example.com", "Alex Chen", auth.RoleBorrower, "credivect-borrower"},
	}
	for _, a := range accounts {
		hash, err := bcrypt.GenerateFromPassword([]byte(a.password), bcrypt.MinCost)
		if err != nil {
			return err
		}
		user := domain.User{ID: a.id, Email: a.email, FullName: a.name, Role: a.role,
			PasswordHash: string(hash), CreatedAt: time.Now().UTC()}
		if err := repo.UpsertUser(ctx, user); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "user %s (%s) ready\n", a.email, a.role)
	}
	return nil
}

func registerModelVersion(ctx context.Context, repo *storage.Repository, scoring *client.ScoringClient) (domain.ModelVersion, error) {
	version := scoring.ModelVersion()
	if version == "" {
		return domain.ModelVersion{}, fmt.Errorf("scoring engine reported no model version")
	}
	sha := datasetChecksum()
	mv := domain.ModelVersion{
		ID: "01918a2b-4c5d-7000-8000-0000000000b1", ModelName: "CrediVect-DualEngine",
		Version: version, Algorithm: "WOE_LIGHTGBM_ENSEMBLE",
		ArtifactPath:  "services/scoring/engine/models/model.txt",
		DatasetSHA256: sha, IsActive: true, DeployedAt: time.Now().UTC(),
	}
	if err := repo.UpsertModelVersion(ctx, mv); err != nil {
		return mv, err
	}
	fmt.Fprintf(os.Stderr, "registered active model %s (dataset %s)\n", version, sha[:12])
	return mv, nil
}

func datasetChecksum() string {
	path := filepath.Join("services", "scoring", "engine", "models", "dataset.sha256")
	raw, err := os.ReadFile(path)
	if err != nil {
		// Container working directory differs; try the mounted artifact path.
		raw, err = os.ReadFile("/etc/credivect/models/dataset.sha256")
		if err != nil {
			return "0000000000000000000000000000000000000000000000000000000000000000"
		}
	}
	fields := string(raw)
	for i := 0; i < len(fields); i++ {
		if fields[i] == ' ' || fields[i] == '\n' {
			return fields[:i]
		}
	}
	return fields
}
