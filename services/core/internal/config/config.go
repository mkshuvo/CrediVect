package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every value the core service reads from the environment.
// Missing infrastructure URLs are fatal: a silently degraded service would
// publish decisions without persistence, without Temporal, or without the
// audit ledger.
type Config struct {
	Port                  string
	ScoringHost           string
	ScoringTO             time.Duration
	PostgresURL           string
	DragonflyURL          string
	KafkaBrokers          []string
	KafkaTopic            string
	TemporalHost          string
	TemporalQueue         string
	AuthMode              string
	JWTSecret             string
	JWTTTL                time.Duration
	SSNKey                string
	CoreOrigin            string
	AllowedOrigins        []string
	NoticeCreditorName    string
	NoticeCreditorAddress string
	NoticeCRAAddress      string
	PSIBaselineFile       string
	RateLimitPerHour      int
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:          getenv("PORT", "8000"),
		ScoringHost:   getenv("SCORING_GRPC_HOST", "localhost:50051"),
		ScoringTO:     envDuration("SCORING_TIMEOUT", 2*time.Second),
		PostgresURL:   os.Getenv("POSTGRES_URL"),
		DragonflyURL:  getenv("DRAGONFLY_URL", "localhost:6379"),
		KafkaTopic:    getenv("KAFKA_TOPIC", "credivect.application.events"),
		TemporalHost:  getenv("TEMPORAL_HOST", "localhost:7233"),
		TemporalQueue: getenv("TEMPORAL_TASK_QUEUE", "credivect-underwriting"),
		AuthMode:      strings.ToLower(getenv("AUTH_MODE", "local")),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		JWTTTL:        envDuration("JWT_TTL", 12*time.Hour),
		SSNKey:        os.Getenv("SSN_ENCRYPTION_KEY"),
		CoreOrigin:    getenv("CORE_ORIGIN", "http://localhost:8000"),

		// An FCRA notice has to name the creditor and the agency that supplied
		// the report, so these are required content, not decoration.
		NoticeCreditorName:    getenv("NOTICE_CREDITOR_NAME", "CrediVect Lending, Inc."),
		NoticeCreditorAddress: getenv("NOTICE_CREDITOR_ADDRESS", "1400 K Street NW, Suite 900, Washington, DC 20005"),
		NoticeCRAAddress:      getenv("NOTICE_CRA_ADDRESS", "P.O. Box 105025, Atlanta, GA 30348"),
		PSIBaselineFile:       getenv("PSI_BASELINE_FILE", "/etc/credivect/models/baseline_deciles.json"),
		RateLimitPerHour:      envInt("RATE_LIMIT_PER_HOUR", 60),
	}
	if cfg.KafkaBrokers = splitCSV(os.Getenv("KAFKA_BROKERS")); len(cfg.KafkaBrokers) == 0 {
		cfg.KafkaBrokers = []string{"localhost:9092"}
	}
	cfg.AllowedOrigins = splitCSV(getenv("ALLOWED_ORIGINS", "http://localhost:3000"))

	if cfg.PostgresURL == "" {
		return nil, fmt.Errorf("POSTGRES_URL is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if cfg.AuthMode != "local" {
		return nil, fmt.Errorf("AUTH_MODE %q is not supported; only the local HS256 provider is implemented", cfg.AuthMode)
	}
	if len(cfg.SSNKey) != 64 {
		return nil, fmt.Errorf("SSN_ENCRYPTION_KEY must be 64 hex characters (32 bytes), got %d", len(cfg.SSNKey))
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	return fallback
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
