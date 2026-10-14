# 06. Underwriting Completion Sequence — CrediVect Platform

This document answers one question precisely: **where does an underwriting case
reach completion, and what is written at that moment?** It exists because
"completed" is not one code path — four different branches all terminate in the
same activity, and only three of them write a decision row.

Line references are to `services/core/internal/workflow/`. The code is
authoritative; if this page and the code disagree, the code is right and this
page is a bug.

---

## 🧭 Phase 1 — the shared spine

Every case runs these four steps before any branching. Each is a separately
retryable, idempotent activity, so a core restart mid-book resumes the case
where it left off rather than losing it.

| # | Stage (`state.Stage`) | Location | Activity | Retry policy |
|---|---|---|---|---|
| 1 | `LOADING_APPLICATION` | `underwriting.go:68-71` | `LoadApplication` | 5 attempts, 1 s → 60 s |
| 2 | `PULLING_BUREAU_REPORT` | `underwriting.go:74-84` | `PullBureauReport` | **6 attempts, 2 s → 2 min** |
| 3 | `COMPUTING_FEATURES` | `underwriting.go:87-90` | `ComputeFeatureVector` | 5 attempts, 1 s → 60 s |
| 4 | `SCORING` | `underwriting.go:93-98` | `ScoreApplication` | 3 attempts from 500 ms |

Step 2 carries the wider policy deliberately: the bureau is the only external
provider in the path, and its failures are transient. Step 4's tight policy
reflects that the scoring engine is in-network. The tier and scorecard points
returned by step 4 are cached onto the query state
(`underwriting.go:99-100`) so `GET /applications/workflow-state` can answer
without re-scoring.

---

## 🔀 Phase 2 — the fork on `result.RecommendedOutcome`

`underwriting.go:102` switches on what the policy engine returned.

| Branch | Location | Status written | Outcome | `DecisionType` | Event |
|---|---|---|---|---|---|
| `APPROVE` | `underwriting.go:103-112` | `APPROVED` | `APPROVED` | `AUTOMATED` | `DECISION_AUTOMATED` |
| `DECLINE` | `underwriting.go:114-122` | `DECLINED` | `DECLINED` | `AUTOMATED` | `DECISION_AUTOMATED` |
| `MANUAL_REVIEW` | `underwriting.go:124-125` | — | — | — | parks, see Phase 3 |
| anything else | `underwriting.go:127-128` | — | — | — | workflow fails: unknown outcome |

The declined branch carries `reason_codes` in its metadata
(`underwriting.go:119`, built by `reasonCodes()` at `:185-191`), which is what
later lets the FCRA notice be regenerated without re-running the model.

---

## ⏸️ Phase 3 — the parked branch (`waitForReviewer`)

`underwriting.go:135-183`. This is the human-in-the-loop fork, and the part most
likely to be misread.

| # | Step | Location |
|---|---|---|
| 5 | Set `AWAITING_REVIEWER` and record when the wait began | `underwriting.go:136-137` |
| 6 | Write status `IN_REVIEW` with **no decision row** | `underwriting.go:139-145` |
| 7 | Race the reviewer signal against the 72-hour timer | `underwriting.go:147-180` |
| 7a | **Signal wins** — `reviewer-decision` received | `underwriting.go:152-167` |
| 7b | **Timer wins** — nobody acted | `underwriting.go:169-178` |

Step 6 sends `DecisionType: ""`, and `outcome.go:46` only inserts a decision row
when `Outcome != ""`. That is why a parked case shows a status but no decision:
the review is a state, not an adjudication.

Step 7a maps `APPROVE` → `APPROVED` and anything else → `DECLINED`, writes
`MANUAL_OVERRIDE`, and records `UNDERWRITER_OVERRIDE_SIGNAL` with the acting
`underwriter_id` in metadata (`underwriting.go:162-164`).

Step 7b declines the applicant on the reasons already recorded
(`underwriting.go:170-176`), event `REVIEW_WINDOW_EXPIRED`, and the workflow
returns `EXPIRED_DECLINED`. The window is `ReviewWindow = 72 * time.Hour`
(`underwriting.go:24`). This is the "no silently abandoned applicant" promise:
an unanswered case still produces a notice.

`selector.Select(ctx)` at `underwriting.go:180` blocks until exactly one of the
two fires, which is what makes the timer cancellable — a reviewer acting after
one minute cuts the timer short instead of leaving it armed for 72 hours.

---

## ✅ Phase 4 — what "completed" actually writes

Every terminal branch funnels into one activity:
`Activities.RecordOutcome` (`outcome.go:32`). It performs four writes in one
place, in this order:

| # | Effect | Location |
|---|---|---|
| 1 | Status transition under optimistic concurrency | `outcome.go:42` |
| 2 | `decisions` row (only when an outcome exists) | `outcome.go:53-67` |
| 3 | Merkle audit append | `outcome.go:85` |
| 4 | Kafka publish of that same ledger entry | `outcome.go:89-93` |

Notes that matter when reading or changing this:

- **Optimistic concurrency.** Step 1 passes `app.Version`
  (`outcome.go:42`). A conflict means another adjudication already landed, which
  is a business outcome rather than a retry — see the comment at
  `outcome.go:29-31`.
- **Actor attribution.** `actor` is `SYSTEM` unless an `UnderwriterID` is
  present, in which case it is `UNDERWRITER` (`outcome.go:77-80`). This is the
  only place the ledger learns who acted.
- **Event naming default.** An empty `EventType` becomes
  `"APPLICATION_" + status` (`outcome.go:81-84`).
- **Single source of stream events.** Step 4 publishes the *audit entry*
  (`outcome.go:90`), so the Kafka stream and the Merkle chain cannot drift apart
  — one is derived from the other.
- The string the workflow returns (`APPROVED` / `DECLINED` /
  `EXPIRED_DECLINED`) becomes the Temporal run result, visible in the Temporal
  UI as the Completed value for `underwriting-<application id>`.

---

## 🔎 Where a completed case can be read back

| Surface | What it shows |
|---|---|
| Temporal UI (`TEMPORAL_UI_HOST_PORT`, default 8233) | Run status, event history, timeline, the returned outcome |
| Postgres `decisions` + `credit_applications.status` | The system of record |
| `audit_ledger` via `GET /api/v1/governance/ledger` | Append-only trail, hash-chained |
| `GET /api/v1/governance/verify` | Recomputes the whole chain from genesis |
| `credivect.application.events` topic in Redpanda Console | The published stream, keyed by application id |
| `GET :8090/events?application_id=` (ingest service) | The consumer's materialised read model |

---

## 🧾 Verification snapshot

Confirmed against a live stack on 2026-10-14/15 (`make train` → `make up` →
`make seed` → browser walkthrough):

- A NEAR_PRIME case parked on the 72-hour timer, was signalled by an
  underwriter, and completed as `APPROVED` in 1m37s with 46 events and 30 state
  transitions in its Temporal history.
- That case produced exactly four stream events, in order:
  `BUREAU_REPORT_RETRIEVED` → `APPLICATION_SCORED` →
  `QUEUED_FOR_MANUAL_REVIEW` → `UNDERWRITER_OVERRIDE_SIGNAL`, and the ingest
  read model returned them at the same Kafka partition/offset the Redpanda
  Console showed.
- Consumer group `credivect-ingest` sat at lag 0.
- Training reproduces the published holdout metrics: AUC 0.8062, KS 0.4586,
  Brier 0.0698.

Related: [03. Step-by-Step Execution Flow](./03_execution_flow_in_chunks.md) for
the narrative version, [04. Financial Engineering](./04_financial_engineering_standards.md)
for the money and concurrency rules this sequence relies on.
