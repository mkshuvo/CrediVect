# CrediVect

Enterprise credit-risk scoring and underwriting platform. A borrower submits an
application, the system pulls a credit file, scores it with two engines (a WoE
scorecard and a monotonic-constrained LightGBM model with TreeSHAP
explanations), applies a lending policy, and either decides automatically or
parks the case for a human underwriter inside a durable Temporal saga. Every
transition is written to an append-only Merkle audit chain, and a declined
applicant gets an FCRA adverse action notice naming the reporting agency, the
score, and the ranked reasons.

This is a working system, not a mock-up: the numbers on screen come from a real
LightGBM booster serving gRPC, the queue is backed by Postgres, the saga is
executed by a Temporal worker, and the audit chain is hash-verified on request.
See [What is real, what is simulated](#what-is-real-what-is-simulated) for the
boundaries.

---

## Architecture

```
                    browser (localhost:3000)
                          |
                          |  /api/v1/* proxied server-side
                          v
 +--------------------+   HTTP    +---------------------+
 |  services/ui       | <-------> |  services/core      |
 |  Next.js 16 React  |           |  Go 1.23 API        |
 |  19 + MUI 6        |           |  + Temporal worker  |
 |  :3000             |           |  :8000              |
 +--------------------+           +----------+----------+
                                             |
              gRPC :50051  <-----------------+------------------> Temporal :7233
      +---------------------+                        (durable saga, 72h review timer)
      |  services/scoring   |
      |  Python 3.12        |          +--------------+  +------------+  +----------+
      |  LightGBM + TreeSHAP|          |  postgres 17 |  |  dragonfly |  | redpanda |
      |  WoE scorecard      |          |  13 tables   |  |  cache     |  |  events  |
      +---------------------+          +--------------+  +------------+  +----------+
```

| Service | Tech | Container port | Default host port | Role |
| --- | --- | --- | --- | --- |
| `ui` | Next.js 16, React 19, MUI 6 | 3000 | 3000 | Borrower wizard, underwriter queue, governance console |
| `core` | Go 1.23 | 8000 | 8000 | REST API, validation, auth, persistence, saga worker, event producer |
| `ingest` | Go 1.23 (same image) | 8090 | 8090 | Kafka consumer group; materialises the event stream and serves it read-only |
| `scoring` | Python 3.12, LightGBM 4 | 50051 | 50051 | PD model, WoE scorecard, TreeSHAP attributions |
| `postgres` | Postgres 17 | 5432 | 5432 | System of record, Temporal store |
| `dragonfly` | Dragonfly 1.40 | 6379 | 6379 | Application/bureau cache, idempotency replay, rate limiting |
| `redpanda` | Redpanda 26.2 (Kafka) | 9092 | 19092 (external) | Application event stream |
| `redpanda-console` | Console 3.11 | 8080 | 8080 | Inspect the event stream |
| `temporal` | auto-setup 1.29.6 | 7233 | 7233 | Durable workflow engine |
| `temporal-ui` | Temporal UI 2.53 | 8080 | 8233 | Inspect workflow histories |
| `seed`, `trainer` | (profiles: `tools`) | - | - | One-off jobs, run via `make` |

The browser only ever talks to the UI origin. `next.config.ts` rewrites
`/api/v1/*` to `CORE_ORIGIN` inside the container network - `docker-compose.yml`
sets it to `http://core:8000` - so the app works whatever host port core happens
to be published on.

### The underwriting saga

`services/core/internal/workflow/underwriting.go`

```
LOADING_APPLICATION -> PULLING_BUREAU_REPORT -> COMPUTING_FEATURES -> SCORING -> DECISIONING
                                   |                                        |
                        retries: 6, 2s backoff                       APPROVE / DECLINE
                        to 2 min ceiling                                   |
                                                                     MANUAL_REVIEW
                                                                           |
                                                          park on a 72h timer, racing
                                                          the `reviewer-decision` signal
                                                                           |
                                              APPROVED / DECLINED / EXPIRED_DECLINED
```

Each step is a separately retryable, idempotent activity, so a core restart
mid-book resumes the case where it left off rather than losing it. An
application nobody reviews expires to a decline on the recorded reasons, which
keeps the "no silently abandoned applicant" promise the audit chain can prove.

### Money, identity and time

- No floating point in money paths. Amounts are `BIGINT` cents, rates and
  probabilities are integer basis points, PD is `NUMERIC(7,6)`. Rounding is
  banker's rounding (`math.RoundToEven` / Python's `ROUND_HALF_EVEN`).
- Identifiers are UUIDv7: time-ordered, monotonic within a process even when the
  clock stalls, so index locality and audit ordering both hold
  (`internal/domain/uuidv7.go`).
- Applications are updated with an optimistic `version` column; bureau files are
  bitemporal (`valid_time` / `transaction_time`), and the raw payload is stored
  with the score so any decision can be reproduced from the file the engine saw.
- The SSN is never stored in the clear: an HMAC-SHA256 blind index allows
  matching, and an AES-256-GCM sealed record allows recovery by a regulated
  process. Both keys derive from `SSN_ENCRYPTION_KEY`
  (`internal/security`).

### Audit chain

`internal/storage/audit_test.go` pins the exact format:

```
entry_hash = SHA256( prev_entry_hash || SHA256(payload_json) || recorded_at_RFC3339Nano )
```

starting from a 64-zero genesis constant, with appends serialised by a
`pg_advisory_xact_lock`. `recorded_at` is truncated to microseconds because
Postgres `TIMESTAMPTZ` is microsecond-precision; storing nanoseconds and
hashing the string would make re-verification fail. `GET
/api/v1/governance/verify` recomputes the whole chain and reports the first
broken record.

### Policy

`services/scoring/engine/policy.py` mirrors the documented tier table:

| Tier | Score floor | Utilisation cap | Action | APR | Advance rate |
| --- | --- | --- | --- | --- | --- |
| SUPER_PRIME | 720 | 250 bps | APPROVE | 7.90% | 100% |
| PRIME | 670 | 500 bps | APPROVE | 9.50% | 100% |
| NEAR_PRIME | 610 | 900 bps | MANUAL_REVIEW | 12.40% | 75% |
| SUBPRIME | 520 | - | DECLINE | 19.99% | 0% |
| HIGH_RISK | 300 | - | DECLINE | 24.99% | 0% |

Bankruptcy on the credit file is a knockout regardless of score.

---

## Quick start

Docker Desktop (with Compose v2) and about 4 GB of free memory. GNU Make is
needed for the command reference below; every target is a one-line `docker
compose` call you can run directly instead.

```bash
git clone git@github.com:mkshuvo:CrediVect.git
cd CrediVect
cp .env.example .env          # then set JWT_SECRET and SSN_ENCRYPTION_KEY

make train                    # ~1 min: generates gitignored engine/models/*
make up                       # start every service, building any missing image
make health                   # wait until every dependency reports UP
make seed                     # originate 240 scored applications through the real saga
```

Open <http://localhost:3000>. Sign in with a seeded account (the login screen
fills them for you):

| Role | Email | Password | Lands on |
| --- | --- | --- | --- |
| Underwriter | `sarah.jenkins@credivect.example` | `credivect-underwriter` | underwriter queue |
| Risk admin | `risk.admin@credivect.example` | `credivect-admin` | queue + governance |
| Borrower | `alex.chen@example.com` | `credivect-borrower` | loan wizard |

The borrower wizard (`/apply`) needs no session at all: fill step 1, size the
loan on step 2, tick the FCRA consent on step 3, and you are redirected to
`/status/<application id>`, which polls itself to a decision.

`make train` is required once on a clean checkout because the trained artifacts
are reproducible from a fixed seed and are therefore not committed. The scoring
image also self-heals (it trains during `docker compose build scoring` if the
artifacts are missing), but `make train` is what writes them back into the
working tree, which is where core reads its PSI baseline from.

### Golden path to walk by hand

1. `/apply` - submit as a fresh borrower.
2. Watch the saga: `/status/<id>` shows `SUBMITTED`, then the review or decision
   card. The Temporal UI (<http://localhost:8233>, namespace `default`, task
   queue `credivect-underwriting`) shows the activity history and retries.
3. If the case is NEAR_PRIME it parks for review. Sign in as the underwriter,
   open the row in `/dashboard`, set terms, send the decision. The borrower page
   flips to an approval within a couple of polls.
4. If the case is declined, the same page renders the Notice of Adverse Action
   and links the plain-text download.
5. Sign in as the risk admin and open `/dashboard/governance`: latency
   percentiles, chain verification, model registry agreement, PSI drift, and the
   ledger entries your steps just appended - including
   `UNDERWRITER_OVERRIDE_SIGNAL` with your user id. An underwriter can read
   every panel too; only the admin role may re-measure PSI, so a drift number
   always traces to a named decision-maker.

---

## Command reference

| Command | What it does |
| --- | --- |
| `make up` | Start the stack detached; builds any image the machine does not have yet |
| `make down` | Stop it (volumes survive) |
| `make build` | Build every image |
| `make rebuild` | Rebuild core, scoring and ui, then force-recreate those three |
| `make ps` | Container status |
| `make logs` | Tail core and scoring logs |
| `make health` | `GET /api/v1/health` from the host |
| `make migrate` | Apply `scripts/init-db.sql` to the running database (idempotent) |
| `make seed` | Run the seeder: users, active model version, `SEED_APPLICATIONS` scored cases |
| `make train` | Retrain the PD model from the fixed seed and write the artifacts |
| `make proto` | Regenerate the Python gRPC stubs from `proto/` |
| `make test` | Both suites: Python scoring tests, then all Go packages |
| `make test-go` | `go test ./...` in `services/core` |
| `make test-scoring` | `unittest discover` inside the scoring image |

Prefer targeted recreates over `make down`: `docker compose up -d
--force-recreate core` restarts one service without releasing the Postgres and
Dragonfly host ports another local stack may already hold.

Useful calls from a terminal. Substitute `CORE_HOST_PORT` from your `.env` for
`8000` if the default is taken (see [Host port
conflicts](#host-port-conflicts)); `make health` already prints the port it
used.

```bash
TOK=$(curl -s -X POST http://localhost:8000/api/v1/auth/login \
  -H 'content-type: application/json' \
  -d '{"email":"risk.admin@credivect.example","password":"credivect-admin"}' \
  | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')

curl -s http://localhost:8000/api/v1/governance/verify     -H "Authorization: Bearer $TOK"
curl -s "http://localhost:8000/api/v1/governance/latency?samples=240" -H "Authorization: Bearer $TOK"
curl -s "http://localhost:8000/api/v1/governance/drift?feature=OVERALL_PD&samples=240" -H "Authorization: Bearer $TOK"
curl -s -X POST "http://localhost:8000/api/v1/governance/drift?feature=OVERALL_PD" -H "Authorization: Bearer $TOK"

# The same events, read back through the consumer (INGEST_HOST_PORT, 8090 by default)
curl -s http://localhost:8090/health
curl -s "http://localhost:8090/events?application_id=<id>&limit=20"
```

---

## API surface

All routes are under `/api/v1` (`/health` is also answering at the root for load
balancers). "Session" means an `Authorization: Bearer` HS256
token; roles are `BORROWER`, `UNDERWRITER`, `ADMIN`.

| Method and path | Gate | Purpose |
| --- | --- | --- |
| `GET /health` and `GET /api/v1/health` | none | Liveness of core and each dependency |
| `POST /auth/login` | none | Issue a 12-hour HS256 session token |
| `GET /auth/me` | any role | Echo the caller's claims |
| `POST /applications` | none, rate limited, `Idempotency-Key` required | Originate an application and start the saga; returns the one-time status token |
| `GET /applications` | UNDERWRITER, ADMIN | Paged queue with `status`, `tier`, `limit`, `offset` and per-status counts |
| `GET /applications/get?id=` | status token or staff session | One application with bureau file, score, SHAP and decision; borrower-identifying detail is stripped for a token caller |
| `POST /applications/review?id=` | UNDERWRITER, ADMIN | Signal the parked saga; returns 202 |
| `GET /applications/adverse-action?id=` | status token or staff session | FCRA notice as JSON |
| `GET /applications/adverse-action.txt?id=` | status token or staff session | The same notice as plain text |
| `GET /applications/workflow-state?id=` | status token or staff session | Temporal query: current stage, tier, waiting-since |
| `GET /governance/ledger?limit=` | UNDERWRITER, ADMIN | Tail of the audit chain |
| `GET /governance/verify` | any role | Recompute and verify every hash link |
| `GET /governance/drift` | UNDERWRITER, ADMIN | Stored PSI evaluations |
| `POST /governance/drift?feature=` | ADMIN | Re-measure PSI against the training baseline |
| `GET /governance/latency?samples=` | UNDERWRITER, ADMIN | Scoring percentiles versus the 15 ms SLA |
| `GET /governance/model` | UNDERWRITER, ADMIN | Registry row plus the version the engine actually serves |

An idempotency key is only consumed by a request that passes validation, so a
rejected submission can be corrected and retried with the same key.

A status token is the 43-character credential `POST /applications` returns once
per case. Send it as `X-Status-Token`, or as `?token=` where the browser has to
fetch it through a link. Core stores only its SHA-256 digest, so neither a table
read nor a database backup can be used to impersonate a borrower.

### Data model

`scripts/init-db.sql` creates 13 tables: `users`, `applicants`,
`credit_applications`, `bureau_reports`, `feature_vectors`, `model_versions`,
`scoring_runs`, `shap_explanations`, `adverse_action_reasons`, `decisions`,
`audit_ledger`, `drift_metrics`, `application_events`. Money columns are `BIGINT`
cents with `CHECK` constraints that match the API's validation bounds, and
`internal/api/validate_test.go` asserts that they stay in step.
`application_events` is keyed on `(kafka_partition, kafka_offset)`, which is what
makes the consumer's at-least-once delivery safe to replay.

---

## Testing

```
$ make test
Ran 20 tests in 0.3s          # services/scoring, in the scoring image
OK
ok  credivect/core/internal/api         # validation bounds, redaction, route gates
ok  credivect/core/internal/auth        # token issue/verify, role gates
ok  credivect/core/internal/bureau      # simulator determinism
ok  credivect/core/internal/governance  # PSI maths
ok  credivect/core/internal/ingest      # event -> row mapping, wire format
ok  credivect/core/internal/security    # AES-GCM + blind index, capability tokens
ok  credivect/core/internal/storage     # Merkle chain vectors
ok  credivect/core/internal/workflow    # saga behaviour
ok  credivect/core/tests                # money/rounding, UUIDv7
```

65 Go test functions plus 20 Python tests, all passing. The interesting ones:

- **Saga tests** (`internal/workflow/saga_test.go`) run the real workflow against
  Temporal's test environment with a mock clock: automatic approval, automatic
  decline with reason codes, a reviewer signal arriving an hour in, the 72-hour
  window expiring to `EXPIRED_DECLINED`, and a bureau provider that fails until
  retries are exhausted.
- **Chain tests** (`internal/storage/audit_test.go`) pin golden hash vectors
  computed out of band, prove every input to the hash matters down to one
  nanosecond, and document why truncation to microseconds is required.
- **Validation tests** (`internal/api/validate_test.go`) walk every documented
  boundary at `n-1`, `n`, `n+1`, assert each error message quotes the bound it
  enforced, and compare the API's bounds against the database `CHECK`s.
- **Scoring tests** (`services/scoring/tests/test_scoring.py`) assert monotonic
  risk in every scored feature, the 300-850 scorecard band, banker's rounding,
  the 15 ms SLA, TreeSHAP completeness (`base_value + sum(attributions) ==
  tree margin`), and that each invalid gRPC field is rejected by name.

- **Route table test** (`internal/api/routes_test.go`) walks the real mux and
  demands the gate answer: every staff route returns 401 anonymously and 403 to a
  borrower session, and a credential that will not parse is rejected on all
  twelve guarded routes. Deleting one `s.guard.RequireRole(...)` wrapper makes it
  fail, which is exactly the mistake that once left the FCRA notice endpoints open.
- **Capability token tests** (`internal/security/token_test.go`,
  `internal/api/status_token_test.go`) check tokens are unguessable and unique,
  that the digest is stable, that only the exact token matches (not a prefix, an
  extension, or one flipped character), that a row with no digest can never
  authenticate, and that a header beats the query string.

### Measured latency

The 15 ms claim is engine time only (gRPC call to decision), measured over the
240 most recent scoring runs on the deployed stack:

| Metric | Value |
| --- | --- |
| p50 | 1.47 ms |
| p95 | 2.52 ms |
| p99 | 3.55 ms |
| max | 5.09 ms |
| mean | 1.58 ms |
| share under 15 ms | 100% |

Live figures at `GET /api/v1/governance/latency` and on the governance screen.
End-to-end application time is dominated by the simulated bureau pull, not by
scoring.

### Continuous integration

`.github/workflows/ci.yml` runs four jobs on every push and pull request: `go`
(gofmt, `go vet`, build, `go test -count=1 ./...`), `scoring` (build the scoring
image, which trains the model from the fixed seed, then run the 20 unittests
inside it), `ui` (`npm ci` and a production `next build`), and `compose` (`docker
compose config` validates the stack file). The commands are the same ones `make
test` and `make up` run locally, and all four pass here; the workflow itself has
not executed on a GitHub runner yet, so treat its first green run as unverified.

---

## What is real, what is simulated

Real and load-bearing:

- LightGBM booster with monotonic constraints, Platt calibration, WoE/IV
  scorecard, and genuine TreeSHAP attributions served over gRPC.
- Postgres persistence, optimistic concurrency, bitemporal bureau snapshots.
- Temporal saga execution with per-activity retry policies, a durable 72-hour
  review timer, signals and queries.
- HS256 sessions with bcrypt-verified passwords and role gates.
- AES-256-GCM SSN sealing plus HMAC blind index.
- Append-only Merkle audit chain, hash-verified on demand.
- PSI drift measurement against the shipped training baseline.
- FCRA adverse action notices, on screen and as a text download.
- Kafka event production on every stage of the saga, and a consumer group that
  materialises that stream into a queryable table.

Simulated or scoped down, deliberately:

- **The credit bureau is a simulator** (`internal/bureau`). It generates
  deterministic, internally consistent consumer files and supports
  `BUREAU_LATENCY_MS` / `BUREAU_FAILURE_RATE` to exercise the retry path. A real
  deployment replaces `bureau.Provider`.
- **The model is trained on synthetic data** (`engine/train.py`, fixed seed
  20260903, 200k applicants, holdout AUC 0.806, KS 0.459, Brier 0.070). It is a
  real gradient-boosted model with a real calibration step, but its predictive
  power says nothing about actual consumer risk.
- **Kafka now has both ends.** `services/core/internal/ingest` runs as the
  `credivect-ingest` service: one consumer group over the application topic,
  committed only after the row lands, deduplicated on the `(partition, offset)`
  primary key. It is a read model for this repo rather than a feed into another
  system - `GET :8090/events?application_id=` is where the stream comes back.
- **Auth is the local HS256 provider only.** `AUTH_MODE` exists to make the
  boundary explicit; no OIDC/SAML integration is wired up.
- Single-node Postgres/Temporal/Redpanda, no TLS between containers, no
  horizontal scaling, and schema changes are one idempotent SQL file rather than
  a migration tool.

---

## Host port conflicts

Docker publishes host ports by default that other local stacks, or Windows
itself, may already own. Two things to know:

- **Windows reserves large dynamic ranges.** Hyper-V / WinNAT can hold blocks
  such as `7209-7308` or `8134-8233`, so binding `8000`, `7233`, `8233` or `8080`
  fails with `bind: An attempt was made to access a socket in a way forbidden by
  its access permissions`. Check with `netsh int ipv4 show excludedportrange
  protocol=tcp` and move the CrediVect ports in `.env` (`CORE_HOST_PORT`,
  `INGEST_HOST_PORT`, `TEMPORAL_HOST_PORT`, `TEMPORAL_UI_HOST_PORT`,
  `REDPANDA_CONSOLE_HOST_PORT`, `KAFKA_EXTERNAL_HOST_PORT`). `docker compose
  down` then `up` releases the
  reservations held by this stack; never take the whole stack down just to free
  Postgres 5432 or Dragonfly 6379 if another project is using them.
- **The browser does not care.** Only the UI port (3000) needs to be reachable;
  `/api/v1/*` is proxied from the UI container to core over the compose network.

---

## Security notes for a real deployment

The repository ships with development placeholders that `docker-compose.yml`
falls back to, and they are committed on purpose so a clean clone boots:

- `JWT_SECRET` and `SSN_ENCRYPTION_KEY` defaults.
- Postgres `credivect_user` / `credivect_password`, shared by core and Temporal.
- Seeded account passwords (`credivect-underwriter`, `credivect-admin`,
  `credivect-borrower`).

Treat all of them as public. Before any non-local deployment: inject real
secrets through the environment or a secret manager, rotate the database
credentials, replace the seeded accounts, and put TLS in front of core. Note
that rotating `SSN_ENCRYPTION_KEY` invalidates both existing sealed SSNs and the
blind indexes used to match borrowers.

The four routes that describe one named consumer - `applications/get`,
`workflow-state`, `adverse-action` and `adverse-action.txt` - require a
credential. Origination is anonymous, so the applicant's credential is the
capability token `POST /applications` returns once; a staff session
(`UNDERWRITER` or `ADMIN`) is accepted on the same routes and sees the complete
record. A token caller additionally gets a redacted projection: the applicant's
email, date of birth, income, external id, employment status and the full
tradeline detail are stripped, leaving the decision, the score and the
explanation the applicant is entitled to see. No credential, no answer: those
routes return 401 rather than a preview.

The token is 256 bits of randomness, stored only as a SHA-256 digest and compared
in constant time, so a leaked table cannot be used to read cases. The UI carries
it in the URL fragment (`/status/<id>#t=...`), which never reaches a server, and
puts it in `sessionStorage` per application so a reload still works. Two limits
remain, and both are deliberate rather than accidental: the token is bearer-grade
(whoever holds it reads that case, and the text download has to put it in a query
string for the anchor to work), and nothing rotates or revokes one - the digest is
written once at origination and carries no expiry. Applications seeded before
capability tokens existed simply have no digest, so they stay unreadable without a
session. A production rollout replaces the token with a borrower account: the same
authorization boundary, with an identity behind it that can be reset.

---

## Layout

```
docker-compose.yml         The pinned ten-service stack plus tool profiles
.env.example               Host ports, secrets and behaviour knobs
Makefile                   Every command above
.github/workflows/ci.yml   Go, scoring image + its tests, UI build, compose config
proto/                     credivect.scoring.v1 gRPC contract
scripts/init-db.sql        Schema, constraints and indexes (idempotent)
services/core/             Go API, repository, saga, activities, auth, audit chain
  cmd/server/              HTTP server + worker bootstrap
  cmd/ingest/              Kafka consumer service and its read-only HTTP API
  cmd/seed/                Seeder (users, model version, applications)
  internal/{api,auth,bureau,cache,client,config,domain,events,governance,ingest,security,storage,workflow}
  tests/                   Money, rounding and UUIDv7 tests
services/scoring/          Python gRPC scoring service
  engine/                  risk.py facade, lightgbm_model, scorecard, policy,
                           features, explain, train
  engine/models/           Trained artifacts (generated, gitignored)
  gen/                     Generated gRPC stubs
  tests/                   Scoring engine and servicer tests
services/ui/               Next.js App Router client
  src/app/                 / (landing), /login, /apply, /dashboard,
                           /dashboard/governance, /status/[id]
  src/components/          ApplicationDetail, UnderwriterReviewPanel, ShapWaterfall
  src/lib/                 api client, formatters, auth context
docs/                      Product vision, domain glossary, execution flow,
                           financial engineering standards, implementation walkthrough
PLAN.md                    Original technical specification
```

`docs/README.md` indexes the specification documents. `docs/00_final_implementation_plan.md`
and `PLAN.md` overlap - the former is the longer-form plan, the latter the
original architecture brief.
