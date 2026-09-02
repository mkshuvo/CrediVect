# 04 — Financial Engineering & Data Invariants

Based on architectural research from **Stripe, Uber (Gulfstream/LedgerStore), and TigerBeetle**, CrediVect strictly enforces five institutional data invariants across all schemas, APIs, and business logic.

---

## 1. The "Zero-Float" Golden Rule (IEEE-754 Ban)

### The Problem with Binary Floating-Point
Binary floating-point types (`float32`, `float64`, `REAL`, `DOUBLE`) cannot precisely represent decimal base-10 fractions. For example:
$$0.1 + 0.2 = 0.30000000000000004$$
In high-volume lending systems, floating-point rounding errors accumulate across millions of calculations, creating balance discrepancies that violate banking accounting standards.

### The CrediVect Rules:
1. **Monetary Amounts**: All currency values (loan amounts, income, balances, limits) are stored as 64-bit signed integers (`BIGINT` in PostgreSQL, `int64` in Go) representing the **minor unit** of the currency (e.g., USD cents: $\$15,000.00$ is stored as `1500000`).
2. **ISO-4217 Currency Standard**: Every monetary amount must be explicitly coupled with a 3-letter currency code (`CHAR(3)` in PostgreSQL, e.g., `USD`, `EUR`, `GBP`).
3. **Interest Rates & Margins in Basis Points (BPS)**:
   - All interest rates, APRs, and rate margins are stored as integer basis points (`INTEGER` in PostgreSQL).
   - $1\text{ Basis Point (bp)} = 0.01\% = 0.0001$.
   - An APR of $12.45\%$ is stored as `1245` bps. This completely eliminates fractional rounding errors in rate tables.
4. **Probabilities & Ratios**:
   - Default probabilities ($PD$) are stored as fixed-point `NUMERIC(7, 6)` ($0.000000$ to $1.000000$) or integer parts-per-million ($PPM$).
   - Ratios ($DTI$, Revolving Utilization) are stored in basis points (`INTEGER`, where $43.5\%$ is `4350` bps).
5. **Rounding Mode**: All division and calibration steps strictly use **Banker's Rounding** (`ROUND_HALF_EVEN` / IEEE 754-2008), eliminating the statistical upward bias present in standard round-half-up math.

---

## 2. Primary Key Architecture: Time-Ordered UUIDv7 (RFC 9562)

Traditional primary key strategies fail in high-scale distributed financial systems:
- **Auto-incrementing integers (`BIGSERIAL`)**: Require a single central database sequence coordinator and leak business transaction volume to outside observers.
- **Random UUIDv4**: Because keys are random, inserting millions of rows scatters writes across random B-Tree index pages, causing severe page fragmentation and cache eviction.

### The UUIDv7 Advantage:
UUIDv7 embeds a 48-bit millisecond Unix timestamp in its most significant bits:
```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           unix_ts_ms                          |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|          unix_ts_ms           |  ver  |       rand_a          |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|var|                        rand_b                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```
- **Sequential Index Locality**: Writes append to the right edge of the B-Tree index, achieving insert speeds matching sequential integers with zero index fragmentation.
- **Distributed Safety**: Independent Go microservices generate keys with zero collision risk without a database roundtrip.
- **Chronological Sorting**: `ORDER BY id` is functionally identical to `ORDER BY created_at`.

---

## 3. Uber LedgerStore Pattern: Immutability & Merkle Chaining

In accordance with banking record-keeping mandates:
1. Core financial tables (`SCORING_RUNS`, `DECISIONS`, `BUREAU_REPORTS`, `AUDIT_LEDGER`) are **strictly append-only**.
2. Database triggers and role permissions bar all `UPDATE` and `DELETE` queries on audit history.
3. **Cryptographic Merkle Chaining**: Every audit entry stores:
   - `payload_sha256` = $\text{SHA-256}(\text{record JSON})$
   - `previous_entry_hash` = Hash of the immediately preceding ledger row
   - `entry_hash` = $\text{SHA-256}(\text{previous\_entry\_hash} + \text{payload\_sha256} + \text{timestamp})$

This establishes a continuous cryptographic chain. Any manual alteration of historical records invalidates the hash of all subsequent entries, providing tamper evidence during audits.

---

## 4. Concurrency & Optimistic Locking (OCC)

Mutable workflow tables (`CREDIT_APPLICATIONS`) enforce Optimistic Concurrency Control using a `version BIGINT` column:
```sql
UPDATE credit_applications 
SET status = $1, version = version + 1, updated_at = NOW() 
WHERE id = $2 AND version = $3;
```
If another process updated the application between read and write, zero rows are modified, allowing the Go service to cleanly detect the collision and handle retries.

---

## 5. Bitemporal Timestamping

All timestamps use `TIMESTAMPTZ` (`TIMESTAMP WITH TIME ZONE`) normalized to UTC.
Bureau and decision records record two distinct timelines:
- **Valid Time**: When the financial event occurred in the real world (e.g., the date the bureau snapshot was compiled).
- **Transaction Time**: When CrediVect recorded the information into the database.

---

## 6. Complete Database Schema (PostgreSQL 17)

```sql
-- 1. Applicants Table
CREATE TABLE applicants (
    id UUID PRIMARY KEY, -- UUIDv7
    external_id VARCHAR(100) UNIQUE NOT NULL,
    ssn_blind_index VARCHAR(64) UNIQUE NOT NULL, -- HMAC-SHA256
    ssn_encrypted BYTEA NOT NULL, -- AES-256-GCM
    first_name VARCHAR(100) NOT NULL,
    last_name VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL,
    date_of_birth DATE NOT NULL,
    monthly_income_cents BIGINT NOT NULL, -- Integer Cents
    currency_code CHAR(3) NOT NULL DEFAULT 'USD',
    employment_status VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Credit Applications Table
CREATE TABLE credit_applications (
    id UUID PRIMARY KEY, -- UUIDv7
    applicant_id UUID NOT NULL REFERENCES applicants(id),
    requested_amount_cents BIGINT NOT NULL, -- Integer Cents
    currency_code CHAR(3) NOT NULL DEFAULT 'USD',
    loan_term_months INTEGER NOT NULL,
    loan_purpose VARCHAR(100) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'SUBMITTED',
    workflow_id VARCHAR(128) NOT NULL,
    version BIGINT NOT NULL DEFAULT 1, -- Optimistic Locking
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. Bureau Reports Table (Append-Only)
CREATE TABLE bureau_reports (
    id UUID PRIMARY KEY, -- UUIDv7
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    bureau_name VARCHAR(50) NOT NULL,
    raw_payload JSONB NOT NULL,
    bureau_score SMALLINT NOT NULL, -- 300-850
    total_debt_cents BIGINT NOT NULL,
    revolving_balance_cents BIGINT NOT NULL,
    revolving_limit_cents BIGINT NOT NULL,
    revolving_utilization_bps INTEGER NOT NULL, -- Basis Points
    delinquencies_24m SMALLINT NOT NULL,
    inquiries_6m SMALLINT NOT NULL,
    total_trades SMALLINT NOT NULL,
    open_trades SMALLINT NOT NULL,
    bankruptcy_flag BOOLEAN NOT NULL DEFAULT FALSE,
    valid_time TIMESTAMPTZ NOT NULL,
    transaction_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 4. Feature Vectors Table
CREATE TABLE feature_vectors (
    id UUID PRIMARY KEY, -- UUIDv7
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    dti_bps INTEGER NOT NULL, -- Basis Points
    revolving_utilization_bps INTEGER NOT NULL, -- Basis Points
    delinquencies_24m SMALLINT NOT NULL,
    inquiries_6m SMALLINT NOT NULL,
    age_of_oldest_trade_months INTEGER NOT NULL,
    income_to_loan_bps INTEGER NOT NULL, -- Basis Points
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 5. Model Versions Table
CREATE TABLE model_versions (
    id UUID PRIMARY KEY,
    model_name VARCHAR(100) NOT NULL,
    version VARCHAR(50) NOT NULL,
    algorithm VARCHAR(50) NOT NULL,
    artifact_path VARCHAR(255) NOT NULL,
    dataset_sha256 CHAR(64) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    deployed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 6. Scoring Runs Table (Append-Only)
CREATE TABLE scoring_runs (
    id UUID PRIMARY KEY, -- UUIDv7
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    model_version_id UUID NOT NULL REFERENCES model_versions(id),
    scorecard_points SMALLINT NOT NULL, -- 300-850
    raw_ml_logit NUMERIC(10, 6) NOT NULL,
    calibrated_pd NUMERIC(7, 6) NOT NULL, -- 0.000000 - 1.000000
    risk_tier VARCHAR(50) NOT NULL,
    execution_time_micros INTEGER NOT NULL,
    scored_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 7. SHAP Explanations Table (Append-Only)
CREATE TABLE shap_explanations (
    id UUID PRIMARY KEY,
    scoring_run_id UUID NOT NULL REFERENCES scoring_runs(id),
    feature_name VARCHAR(100) NOT NULL,
    feature_value NUMERIC(14, 4) NOT NULL,
    shap_value NUMERIC(10, 6) NOT NULL,
    rank SMALLINT NOT NULL
);

-- 8. Adverse Action Reasons Table (Append-Only)
CREATE TABLE adverse_action_reasons (
    id UUID PRIMARY KEY,
    scoring_run_id UUID NOT NULL REFERENCES scoring_runs(id),
    reason_code CHAR(4) NOT NULL, -- e.g. AA01
    description VARCHAR(255) NOT NULL,
    priority_rank SMALLINT NOT NULL
);

-- 9. Decisions Table (Append-Only)
CREATE TABLE decisions (
    id UUID PRIMARY KEY, -- UUIDv7
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    scoring_run_id UUID NOT NULL REFERENCES scoring_runs(id),
    outcome VARCHAR(50) NOT NULL, -- APPROVED | DECLINED | REFERRED
    approved_limit_cents BIGINT NOT NULL DEFAULT 0,
    approved_apr_bps INTEGER NOT NULL DEFAULT 0,
    decision_type VARCHAR(50) NOT NULL, -- AUTOMATED | MANUAL_OVERRIDE
    underwriter_id UUID,
    decision_notes TEXT,
    decisioned_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 10. Audit Ledger Table (Append-Only Cryptographic Merkle Chain)
CREATE TABLE audit_ledger (
    id UUID PRIMARY KEY, -- UUIDv7
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    event_type VARCHAR(100) NOT NULL,
    actor VARCHAR(100) NOT NULL,
    payload_sha256 CHAR(64) NOT NULL,
    previous_entry_hash CHAR(64) NOT NULL,
    entry_hash CHAR(64) NOT NULL,
    metadata JSONB NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
