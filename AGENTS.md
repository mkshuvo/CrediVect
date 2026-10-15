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
- The landing page used to advertise "the top 4 ranked reason codes". Four is a
  **ceiling, not a floor** — a declined case can legitimately render one
  reason. Verified live on 2026-10-15 and the copy was corrected the same day.

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
- **2026-10-15** — `make test` green locally: 13 Go packages plus the Python
  suite. With CI off, this is the gate.
- **2026-10-15, defect found and fixed: `make e2e` could not run locally.**
  `cmd/e2e` defaulted to `http://localhost:8600`, a port that exists nowhere
  else in the repo (compose publishes `${CORE_HOST_PORT:-8000}:8000`). CI hid it
  by setting `CORE_URL` explicitly in `ci.yml`, so the job was green while the
  documented local path failed on its very first check. Fixed by defaulting to
  8000 and exporting `CORE_URL` from the Makefile's `CORE_PORT`. First local
  run afterwards: **42 checks passed, 0 failed** (chain verified over 977
  entries, p95 7.39 ms against the 15 ms SLA). The 11 OIDC checks still SKIP
  without `make idp` + `AUTH_MODE=oidc`.
- **2026-10-15, defect found and fixed: the underwriter queue was
  mouse-only.** Each `TableRow` in `services/ui/src/app/dashboard/page.tsx`
  carried `onClick` and `cursor: pointer` with no `tabIndex`, key handler or
  accessible name, so a keyboard or screen-reader user could not open a case —
  and opening a case is the whole job. Verified in the running browser first
  (the accessibility tree exposed the borrower's name as inert text, and a
  synthetic click on it failed) before touching the code. Rows are now
  focusable, respond to Enter and Space, and carry an aria-label naming the
  borrower and status; re-verified live by focusing a parked case and pressing
  Enter, which opened the decision panel.
- **2026-10-15, audited and found sound** (no change needed, recorded so the
  next session does not redo it): list and governance bounds clamp rather than
  dump (`applications?limit=-5` and `limit=100000` both return the 50-row
  default; `governance/ledger?limit=999999999` returns 100; `latency?samples=-1`
  clamps to the population); an unknown `status`/`tier` filter returns an empty
  set rather than silently ignoring the filter; SQL-injection and malformed ids
  in `?id=` return 404 with no query reached; signalling an already-decided case
  returns 400, not 202; `/me/applications` as ADMIN returns 403; a POST without
  `Idempotency-Key` is 422. Every UI page surfaces fetch failures through an
  `Alert` backed by a real `catch` — no silent failures.
- **2026-10-15, defect found and fixed: the delegated sign-in demo could not
  authenticate.** `cmd/dev-idp` stamps `iss` with its own listening address by
  default, while README:202-206 has core verify against the
  `host.docker.internal` form (the only address the container can reach it by).
  The mismatch made every federated login a 401. Fixed in the Makefile by
  deriving `IDP_ISSUER` from `IDP_ADDR`; the target also gained an address
  override because Windows reserves 9340-9439 on this machine, swallowing the
  hardcoded 9355.
- **2026-10-15, the 11 OIDC e2e checks have never run in CI.** `ci.yml` starts
  no `dev-idp` and sets no `OIDC_*`, so `cmd/e2e` prints its SKIP line there and
  the job goes green having checked 42 of 53. The README's "53 checks passed"
  was therefore never demonstrated on a runner. Verified locally on
  `IDP_ADDR=127.0.0.1:9455`: **53 passed, 0 failed**, covering federated sign-in,
  UUIDv5 account provisioning, a roleless token answering 403 rather than 401, a
  tampered signature rejected, local logins still working beside the provider,
  and a federated borrower owning the case they filed. Reproduce with:
  `make idp IDP_ADDR=127.0.0.1:9455`, recreate core with
  `AUTH_MODE=oidc OIDC_ISSUER=http://host.docker.internal:9455
  OIDC_JWKS_URL=http://host.docker.internal:9455/.well-known/jwks.json
  OIDC_ALLOW_CLEARTEXT_KEYS=true`, then
  `E2E_IDP_URL=http://127.0.0.1:9455 make e2e`.
- **2026-10-15, latency caveat.** Under the concurrent load of an image build
  and an e2e run on this laptop, `governance/latency` reported p99 72 ms while
  the SLA check still passed, because it asserts the *share* of runs under
  15 ms rather than the tail. Do not read an inflated p99 during local work as a
  regression; the quiet figures were p50 5.1 ms, p95 7-8 ms.
- **2026-10-15, defect found and fixed: `test_scoring_stays_under_the_15ms_sla`
  was a flaky gate.** It asserted a single median over 60 scores, so it failed
  (20 ms median) whenever anything else was using the CPU -- it broke while a
  Temporal replay was running, and passed minutes earlier on the same code. The
  engine itself medians 998 us in a quiet container. Now takes the best of three
  passes, which a neighbour cannot beat but a genuinely slower model can:
  verified by injecting a 40x slowdown, which still fails at 937 ms median. Its
  comment also claimed the measured list excluded cold runs; it did not. This
  matches the policy the Go benchmarks already follow -- report timing, do not
  gate on it.
- **2026-10-15, defect found and fixed: the governance PSI chip reported
  moderate drift as critical.** `governance/drift.go:88-96` returns `STABLE`,
  `MODERATE_DRIFT` and `CRITICAL_DRIFT`, but the chip in
  `dashboard/governance/page.tsx` compared against `'MODERATE'`, which never
  matches -- so every moderate reading fell through to the error colour. The
  severity tiering is the entire point of that panel. Verified fixed in the
  browser: the chip now computes to the warning amber `rgb(245,158,11)`.
  Cross-checked every other status literal the UI compares against
  `domain/models.go` (`APPROVED`, `DECLINED`, `IN_REVIEW`, `SUBMITTED`,
  `CANCELLED`, and the `APPROVE` signal value) -- all match, so this was the
  only instance.
- **2026-10-15, defect found and fixed: wide tables clipped instead of
  scrolling.** MUI `Paper` is `overflow: hidden`, and the six-column queue
  (577 px intrinsic) and drift (1302 px) tables had no scroll container, so on a
  narrow viewport their right-hand columns vanished with no way to reach them.
  The ledger table already wrapped itself in `overflowX: 'auto'`; the other two
  had simply been missed. Used the same one-line idiom rather than
  `TableContainer`, which contributes nothing but `overflow-x: auto` and would
  have re-indented 60 lines.
- **2026-10-15, UI contrast and overflow defects fixed.** Measured in the
  rendered page, not estimated: the TreeSHAP waterfall drew its "Score Boost" /
  "Risk Penalty" labels in `success.main` (#10B981, **2.54:1**) and
  `error.main` (#EF4444, **3.76:1**) on white -- both far below the 4.5:1 floor,
  on the text that carries the legally required explanation. Now #047857 (5.48)
  and #B91C1C (6.47); the bars keep the bright hues because a graphic has no
  contrast obligation. Separately, the shared `Navbar` needed ~910px of content
  and forced a horizontal scrollbar on every page at a 731px viewport; labels
  now collapse to icons below `sm` (each button keeps an `aria-label`) and the
  decorative chips drop out below `md`. Page scroll width went 912 -> 731.
  Note `theme.ts` has no AA-passing green: `success.dark` (#059669) is 3.76:1.
