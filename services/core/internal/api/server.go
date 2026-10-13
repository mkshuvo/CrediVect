package api

import (
	"net/http"
	"strings"
	"time"

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

// Server wires the HTTP boundary to the real dependencies. Nothing in a handler
// computes risk, mints a hash, or holds state: it validates, delegates and
// serialises.
type Server struct {
	repo       *storage.Repository
	cache      *cache.DragonflyClient
	scoring    *client.ScoringClient
	temporal   *workflow.Runtime
	publisher  *events.Publisher
	tokens     *auth.TokenService
	guard      *auth.Middleware
	hasher     *security.Hasher
	cfg        *config.Config
	bureauName string
	rateLimit  int
	rateWindow time.Duration
}

func NewServer(repo *storage.Repository, c *cache.DragonflyClient, sc *client.ScoringClient,
	runtime *workflow.Runtime, publisher *events.Publisher, tokens *auth.TokenService,
	hasher *security.Hasher, cfg *config.Config) *Server {

	return &Server{
		repo:       repo,
		cache:      c,
		scoring:    sc,
		temporal:   runtime,
		publisher:  publisher,
		tokens:     tokens,
		guard:      auth.NewMiddleware(tokens),
		hasher:     hasher,
		cfg:        cfg,
		bureauName: bureau.NewDeterministicProvider().Name(),
		rateLimit:  cfg.RateLimitPerHour,
		rateWindow: time.Hour,
	}
}

// Routes registers every endpoint with its role gate.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.HandleHealth)
	mux.HandleFunc("GET /api/v1/health", s.HandleHealth)

	mux.HandleFunc("POST /api/v1/auth/login", s.HandleLogin)
	mux.Handle("GET /api/v1/auth/me", s.guard.RequireRole(auth.RoleBorrower, auth.RoleUnderwriter, auth.RoleAdmin)(http.HandlerFunc(s.HandleMe)))

	mux.HandleFunc("POST /api/v1/applications", s.HandleCreateApplication)
	mux.Handle("GET /api/v1/applications", s.guard.RequireRole(auth.RoleUnderwriter, auth.RoleAdmin)(http.HandlerFunc(s.HandleListApplications)))
	// These four each describe one named consumer. Origination is anonymous, so
	// the applicant's credential is the capability token returned by
	// POST /applications rather than a session; Authenticate lets an underwriter
	// or admin session in through the same route, and the handler decides which
	// one it saw. Neither credential means neither answer.
	mux.Handle("GET /api/v1/applications/get", s.guard.Authenticate(http.HandlerFunc(s.HandleGetApplication)))
	mux.Handle("POST /api/v1/applications/review", s.guard.RequireRole(auth.RoleUnderwriter, auth.RoleAdmin)(http.HandlerFunc(s.HandleReviewDecision)))
	mux.Handle("GET /api/v1/applications/adverse-action", s.guard.Authenticate(http.HandlerFunc(s.HandleAdverseAction)))
	mux.Handle("GET /api/v1/applications/adverse-action.txt", s.guard.Authenticate(http.HandlerFunc(s.HandleAdverseActionNotice)))
	mux.Handle("GET /api/v1/applications/workflow-state", s.guard.Authenticate(http.HandlerFunc(s.HandleWorkflowState)))

	mux.Handle("GET /api/v1/governance/ledger", s.guard.RequireRole(auth.RoleUnderwriter, auth.RoleAdmin)(http.HandlerFunc(s.HandleLedger)))
	mux.Handle("GET /api/v1/governance/verify", s.guard.RequireRole(auth.RoleUnderwriter, auth.RoleAdmin, auth.RoleBorrower)(http.HandlerFunc(s.HandleVerifyChain)))
	mux.Handle("GET /api/v1/governance/drift", s.guard.RequireRole(auth.RoleUnderwriter, auth.RoleAdmin)(http.HandlerFunc(s.HandleDriftList)))
	mux.Handle("POST /api/v1/governance/drift", s.guard.RequireRole(auth.RoleAdmin)(http.HandlerFunc(s.HandleDriftEvaluate)))
	mux.Handle("GET /api/v1/governance/latency", s.guard.RequireRole(auth.RoleUnderwriter, auth.RoleAdmin)(http.HandlerFunc(s.HandleLatency)))
	mux.Handle("GET /api/v1/governance/model", s.guard.RequireRole(auth.RoleUnderwriter, auth.RoleAdmin)(http.HandlerFunc(s.HandleModelVersion)))

	return mux
}

// CORS restricts credentialed origins to the configured allowlist; a wildcard
// with credentials is not a valid CORS response and would let any page call the
// underwriting API with a stolen token.
func (s *Server) CORS(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range s.cfg.AllowedOrigins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimRight(r.Header.Get("Origin"), "/")
		if origin != "" && allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
