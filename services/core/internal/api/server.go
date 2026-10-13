package api

import (
	"net/http"
	"strings"
	"sync"
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
	repo      *storage.Repository
	cache     *cache.DragonflyClient
	scoring   *client.ScoringClient
	temporal  *workflow.Runtime
	publisher *events.Publisher
	tokens    *auth.TokenService
	guard     *auth.Middleware
	// federated remembers which externally authenticated subjects already have
	// an account row, so a burst of requests from one signed-in person does not
	// become a burst of writes. It is not an authorisation cache: the role in
	// every response comes from the token just verified, so an identity provider
	// that drops a role takes it away on the next request.
	federated  sync.Map
	hasher     *security.Hasher
	cfg        *config.Config
	bureauName string
	rateLimit  int
	rateWindow time.Duration
}

func NewServer(repo *storage.Repository, c *cache.DragonflyClient, sc *client.ScoringClient,
	runtime *workflow.Runtime, publisher *events.Publisher, tokens *auth.TokenService,
	verifiers []auth.Verifier, hasher *security.Hasher, cfg *config.Config) *Server {

	return &Server{
		repo:       repo,
		cache:      c,
		scoring:    sc,
		temporal:   runtime,
		publisher:  publisher,
		tokens:     tokens,
		guard:      auth.NewMiddleware(verifiers...),
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

	// Origination stays open to anonymous callers, but Authenticate still runs:
	// a session that is present has to be visible to the handler, because an
	// application filed while signed in is what binds the account to the credit
	// record. A bearer token that does not verify is rejected rather than
	// silently downgraded to anonymous, which would book the case and lose its
	// owner without telling anyone.
	mux.Handle("POST /api/v1/applications", s.guard.Authenticate(http.HandlerFunc(s.HandleCreateApplication)))
	mux.Handle("GET /api/v1/applications", s.guard.RequireRole(auth.RoleUnderwriter, auth.RoleAdmin)(http.HandlerFunc(s.HandleListApplications)))
	// A borrower's own queue, scoped by the applicant record bound to the session.
	mux.Handle("GET /api/v1/me/applications", s.guard.RequireRole(auth.RoleBorrower)(http.HandlerFunc(s.HandleMyApplications)))

	// These four each describe one named consumer. Origination is anonymous, so
	// an applicant who never signed in holds the capability token returned by
	// POST /applications; a borrower session that owns the record is the other
	// way in, and Authenticate lets staff through the same route. The handler
	// decides which of the three it saw, and how much to show. No credential, no
	// answer.
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
