# AGENTS.md — project knowledge for AI sessions

Read this before working in CrediVect. It records what has been **verified by
running the system**, which is different from what the design documents assert.

## Authority order

When sources disagree, trust them in this order:

1. **Code** — `services/core`, `services/scoring`, `services/ui`
2. **`README.md`** — maintained against the code, accurate as of 2026-10-14
3. **`docs/`** — original design briefs; several sections are stale (listed below)
4. **`PLAN.md`** — the earliest spec, superseded in places by all of the above

`docs/00_final_implementation_plan.md` and `PLAN.md` overlap deliberately;
neither is a maintenance target.

## Where the important things live

| Concern | File |
|---|---|
| Saga definition, the review fork, terminal outcomes | `services/core/internal/workflow/underwriting.go` |
| What "completed" writes (status, decision, ledger, Kafka) | `services/core/internal/workflow/outcome.go` |
| Lending policy and tier thresholds | `services/scoring/engine/policy.py` |
| Audit hash format | `services/core/internal/storage/audit_test.go` |
| Route + role gates | `services/core/internal/api/routes_test.go` |
| Borrower redaction contract | `services/core/internal/api/applications_test.go` |
| OIDC verification | `services/core/internal/auth/jwks.go` |
| Schema (13 tables) | `scripts/init-db.sql` |
| End-to-end client of the running stack | `services/core/cmd/e2e` |

`docs/06_underwriting_completion_sequence.md` walks the saga to its four
completion points with line references.

## Invariants worth preserving

These are the load-bearing promises. Breaking one is a real defect, not a
cleanup detail.

- **No floats in money paths.** Amounts are integer cents, rates and
  probabilities are basis points, PD is `NUMERIC(7,6)`. Rounding is
  banker's rounding (`math.RoundToEven`, Python `ROUND_HALF_EVEN`).
- **The API's validation bounds must match the database `CHECK`s** —
  `internal/api/validate_test.go` asserts this.
- **Audit chain format is pinned by golden vectors:**
  `SHA256(prev || SHA256(payload_json) || recorded_at_RFC3339Nano)`, genesis is
  64 zeros, and `recorded_at` is truncated to microseconds because Postgres
  `TIMESTAMPTZ` is microsecond precision. Storing nanoseconds and hashing the
  string makes re-verification fail.
- **A status token is accepted only as `X-Status-Token`.** The query string is
  ignored, not honoured — a credential in a URL reaches access logs and browser
  history. Core stores only its SHA-256 digest.
- **Ownership is resolved from the database on every request**, never from a
  claim inside the session token. No endpoint accepts a claimed identity after
  the fact.
- **Federated account ids are derived, not stored:** UUIDv5 of
  `issuer + 0x00 + sub`. Their password hash is `"!"`, which bcrypt can never
  match.
- **Every parked case terminates.** The 72-hour timer declines the applicant on
  the recorded reasons rather than leaving the case open.
- **Kafka events are derived from the audit entry**, so the stream and the
  Merkle chain cannot drift apart.

## Known-stale documentation

Do not restate these as fact; fix the doc if you touch it.

- `docs/02_terminology_and_glossary.md:42-46` teaches tiers 780/720/660/600.
  The real table is **720 / 670 / 610 / 520** with PD caps 250/500/900 bps —
  `services/scoring/engine/policy.py:26-30`.
- `docs/05_implementation_walkthrough.md` claims "100% implemented", "11
  tables" (there are 13), and omits the `ingest` consumer service and
  `/my-applications`. It also cites a fixed "707 µs" latency that no longer
  matches anything.
- `docs/05_implementation_walkthrough.md:5` and `:14` say the build lives in
  `D:\projects\CrediVect`. The repo is not on that drive.
- `docs/01` and `docs/03` disagree on the Alex Chen story ($12,000 vs $10,000,
  48h vs 72h timer). The code is 72 hours.
- The landing page advertises "the top 4 ranked reason codes". Four is a
  **ceiling, not a floor** — a declined case can legitimately render one
  reason. Verified live on 2026-10-15.

## Commands

`make up` · `down` · `build` · `rebuild` · `ps` · `logs` · `health` ·
`migrate` · `seed` · `train` · `proto` · `test` · `test-go` · `test-scoring` ·
`bench` · `e2e` · `idp`

`make train` is required once on a clean checkout: the trained artifacts under
`services/scoring/engine/models/` are gitignored and reproducible from seed
20260903. Core reads its PSI baseline from that directory, so `train` must run
on the host-visible tree, not only inside the image.

Prefer `docker compose up -d --force-recreate <one service>` over `make down`:
releasing the whole stack drops the Postgres and Dragonfly host ports another
local stack may hold.

## Environment notes for this development machine

Recorded because they cost real time to rediscover. None of them are properties
of the repository.

- **Host port collisions.** `vex-postgres` owns 5432 and `vex-api` owns 8080, so
  `.env` sets `POSTGRES_HOST_PORT=5433` and `REDPANDA_CONSOLE_HOST_PORT=8081`.
  The Postgres mapping was hardcoded `"5432:5432"`; `docker-compose.yml:190` now
  parameterises it as `${POSTGRES_HOST_PORT:-5432}:5432`.
- **Two registries do not resolve from this network:** `docker.redpanda.com` and
  `docker.dragonflydb.io`. Work around it by pulling the identical pinned tags
  from Docker Hub (`redpandadata/redpanda`, `redpandadata/console`) and
  `ghcr.io/dragonflydb/dragonfly`, then `docker tag`-ing them to the exact
  references compose expects. No compose change needed.
- **`grpcio-tools>=1.70.0` fails to resolve intermittently** during the scoring
  build ("from versions: none") while `grpcio` downloads fine. It is a
  network/PyPI flake — rebuild before touching `requirements.txt`.
- **The UI image build needs `services/ui/public/` to exist.**
  `services/ui/Dockerfile:29` copies `/app/public`. Nothing under `src/`
  references a public asset (`src/app/icon.svg` covers the favicon), so the
  cleaner upstream fix is deleting that COPY line. CI's `ui` job runs
  `npm ci` + `next build` directly and never exercises the Dockerfile, which is
  how this stayed green while `make up` was broken on a clean checkout.

## Verified state log

Append dated entries here when something is actually run and checked. Delete or
correct entries that later prove wrong.

- **2026-10-14, HEAD `ba80ec2`** — all five CI jobs green on GitHub
  (`mkshuvo/CrediVect`, run 38469980697), including **End-to-end loan (live
  stack)**. `README.md:509-511` still claims that job "has not executed on a
  GitHub runner yet"; that caveat is stale.
- **2026-10-14** — full cold start verified: `train` (AUC 0.8062, KS 0.4586,
  Brier 0.0698) → `up` (10 services healthy) → `seed` (240 applications;
  book settled 66 approved / 126 declined / 48 in review).
- **2026-10-15** — golden path driven in a browser end to end. A NEAR_PRIME
  case parked on the 72h timer, was approved by an underwriter signal, and the
  borrower page flipped to approved. Governance: chain verified across 968
  entries, p95 7.25 ms with 100% inside the 15 ms SLA, PSI 0.1668
  `MODERATE_DRIFT`. RBAC confirmed live: `POST /governance/drift` as
  UNDERWRITER → **403**, ADMIN → measured. Redaction confirmed: a token-only
  caller gets `email ""`, `dob ""`, `income 0`, `employment ""`, no raw bureau
  payload, but keeps score, tier, PD and reasons. Cross-account reads → 401,
  borrower opening the staff queue → 403, anonymous read → 401.
- **2026-10-15** — Temporal UI showed 241 workflows (46 Running / 195
  Completed); the 46 running matched the queue's 46 "awaiting action" exactly.
  Consumer group `credivect-ingest` at lag 0, and `:8090/events` returned the
  same four events at the same Kafka offsets Redpanda Console displayed.
- **2026-10-15** — run `38617558575` on `b42954d` passed **all five jobs**, the
  first CI run to exercise the parameterised Postgres port and the new
  `services/ui/public/` directory.
- **2026-10-15, CI is currently DISABLED for this repository** at the owner's
  request: `gh api -X PUT repos/mkshuvo/CrediVect/actions/permissions -F
  enabled=false`. `.github/workflows/ci.yml` is intentionally left in place, so
  no runs happen on push or PR. Re-enable with the same call and
  `-F enabled=true`. Note the premise was cost, but the repo is **public**, and
  Actions on public repos is free and unlimited on GitHub-hosted runners — so
  nothing here was being billed. Treat CI as unavailable until someone flips it
  back, and run `make test` / `make e2e` locally instead.
