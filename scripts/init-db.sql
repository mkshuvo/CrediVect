-- CrediVect relational schema.
-- Financial data standard: money is BIGINT minor units, rates and probabilities
-- are integers or fixed point. No floating point money anywhere.
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL CHECK (position('@' in email) > 1),
    full_name VARCHAR(200) NOT NULL,
    role VARCHAR(20) NOT NULL CHECK (role IN ('BORROWER', 'UNDERWRITER', 'ADMIN')),
    password_hash VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS applicants (
    id UUID PRIMARY KEY,
    external_id VARCHAR(100) UNIQUE NOT NULL,
    ssn_blind_index VARCHAR(64) UNIQUE NOT NULL,
    ssn_encrypted BYTEA NOT NULL,
    first_name VARCHAR(100) NOT NULL CHECK (char_length(first_name) >= 2),
    last_name VARCHAR(100) NOT NULL CHECK (char_length(last_name) >= 2),
    email VARCHAR(255) NOT NULL CHECK (position('@' in email) > 1),
    date_of_birth DATE NOT NULL CHECK (date_of_birth < CURRENT_DATE),
    monthly_income_cents BIGINT NOT NULL CHECK (monthly_income_cents >= 50000 AND monthly_income_cents <= 50000000),
    currency_code CHAR(3) NOT NULL DEFAULT 'USD',
    employment_status VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS credit_applications (
    id UUID PRIMARY KEY,
    applicant_id UUID NOT NULL REFERENCES applicants(id),
    requested_amount_cents BIGINT NOT NULL CHECK (requested_amount_cents BETWEEN 100000 AND 50000000),
    currency_code CHAR(3) NOT NULL DEFAULT 'USD',
    loan_term_months INTEGER NOT NULL CHECK (loan_term_months BETWEEN 6 AND 84),
    loan_purpose VARCHAR(100) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'SUBMITTED'
        CHECK (status IN ('SUBMITTED', 'IN_REVIEW', 'APPROVED', 'DECLINED', 'CANCELLED')),
    workflow_id VARCHAR(128) NOT NULL,
    -- SHA-256 of the capability token handed to the applicant at submission.
    -- Only the digest is stored, so the table alone cannot be replayed as
    -- access to a borrower's status page or adverse action notice.
    status_token_hash TEXT,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_credit_apps_applicant ON credit_applications(applicant_id);
CREATE INDEX IF NOT EXISTS idx_credit_apps_status ON credit_applications(status);
CREATE INDEX IF NOT EXISTS idx_credit_apps_created ON credit_applications(created_at DESC);

-- Idempotent addition for databases created before borrower reads were
-- capability-gated; the CREATE TABLE above carries it for a fresh install.
-- Rows predating this have no digest and so allow no anonymous read at all.
ALTER TABLE credit_applications ADD COLUMN IF NOT EXISTS status_token_hash TEXT;

CREATE TABLE IF NOT EXISTS bureau_reports (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    bureau_name VARCHAR(50) NOT NULL,
    raw_payload JSONB NOT NULL,
    bureau_score SMALLINT NOT NULL CHECK (bureau_score BETWEEN 300 AND 850),
    total_debt_cents BIGINT NOT NULL CHECK (total_debt_cents >= 0),
    revolving_balance_cents BIGINT NOT NULL CHECK (revolving_balance_cents >= 0),
    revolving_limit_cents BIGINT NOT NULL CHECK (revolving_limit_cents >= 0),
    revolving_utilization_bps INTEGER NOT NULL CHECK (revolving_utilization_bps BETWEEN 0 AND 100000),
    delinquencies_24m SMALLINT NOT NULL DEFAULT 0 CHECK (delinquencies_24m >= 0),
    inquiries_6m SMALLINT NOT NULL DEFAULT 0 CHECK (inquiries_6m >= 0),
    total_trades SMALLINT NOT NULL DEFAULT 0 CHECK (total_trades >= 0),
    open_trades SMALLINT NOT NULL DEFAULT 0 CHECK (open_trades >= 0),
    bankruptcy_flag BOOLEAN NOT NULL DEFAULT FALSE,
    age_of_oldest_trade_months INTEGER NOT NULL DEFAULT 0 CHECK (age_of_oldest_trade_months BETWEEN 0 AND 1200),
    valid_time TIMESTAMPTZ NOT NULL,
    transaction_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bureau_reports_app ON bureau_reports(application_id);

CREATE TABLE IF NOT EXISTS feature_vectors (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    dti_bps INTEGER NOT NULL CHECK (dti_bps >= 0),
    revolving_utilization_bps INTEGER NOT NULL CHECK (revolving_utilization_bps >= 0),
    delinquencies_24m SMALLINT NOT NULL,
    inquiries_6m SMALLINT NOT NULL,
    age_of_oldest_trade_months INTEGER NOT NULL,
    income_to_loan_bps INTEGER NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_feature_vectors_app ON feature_vectors(application_id);

CREATE TABLE IF NOT EXISTS model_versions (
    id UUID PRIMARY KEY,
    model_name VARCHAR(100) NOT NULL,
    version VARCHAR(50) NOT NULL,
    algorithm VARCHAR(50) NOT NULL,
    artifact_path VARCHAR(255) NOT NULL,
    dataset_sha256 CHAR(64) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    deployed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS scoring_runs (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    model_version_id UUID NOT NULL REFERENCES model_versions(id),
    scorecard_points SMALLINT NOT NULL CHECK (scorecard_points BETWEEN 300 AND 850),
    raw_ml_logit NUMERIC(10, 6) NOT NULL,
    calibrated_pd NUMERIC(7, 6) NOT NULL CHECK (calibrated_pd BETWEEN 0.000000 AND 1.000000),
    pd_basis_points INTEGER NOT NULL CHECK (pd_basis_points >= 0),
    risk_tier VARCHAR(50) NOT NULL,
    -- The engine's recommended terms are part of the score output and must be
    -- reproducible: an underwriter override is audited against what the model
    -- actually asked for.
    recommended_outcome VARCHAR(20) NOT NULL,
    max_approved_limit_cents BIGINT NOT NULL DEFAULT 0 CHECK (max_approved_limit_cents >= 0),
    recommended_apr_bps INTEGER NOT NULL DEFAULT 0 CHECK (recommended_apr_bps >= 0),
    execution_time_micros INTEGER NOT NULL CHECK (execution_time_micros >= 0),
    scored_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_scoring_runs_app ON scoring_runs(application_id);
CREATE INDEX IF NOT EXISTS idx_scoring_runs_model ON scoring_runs(model_version_id, scored_at DESC);

-- Idempotent additions for databases created before the recommended terms were
-- persisted; the CREATE TABLE above carries them for a fresh install.
ALTER TABLE scoring_runs ADD COLUMN IF NOT EXISTS pd_basis_points INTEGER NOT NULL DEFAULT 0;
ALTER TABLE scoring_runs ADD COLUMN IF NOT EXISTS recommended_outcome VARCHAR(20) NOT NULL DEFAULT 'DECLINE';
ALTER TABLE scoring_runs ADD COLUMN IF NOT EXISTS max_approved_limit_cents BIGINT NOT NULL DEFAULT 0;
ALTER TABLE scoring_runs ADD COLUMN IF NOT EXISTS recommended_apr_bps INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS shap_explanations (
    id UUID PRIMARY KEY,
    scoring_run_id UUID NOT NULL REFERENCES scoring_runs(id),
    feature_name VARCHAR(100) NOT NULL,
    feature_value VARCHAR(32) NOT NULL,
    shap_value NUMERIC(10, 6) NOT NULL,
    rank SMALLINT NOT NULL CHECK (rank >= 1)
);

CREATE INDEX IF NOT EXISTS idx_shap_run ON shap_explanations(scoring_run_id);

CREATE TABLE IF NOT EXISTS adverse_action_reasons (
    id UUID PRIMARY KEY,
    scoring_run_id UUID NOT NULL REFERENCES scoring_runs(id),
    reason_code CHAR(4) NOT NULL CHECK (reason_code ~ '^AA[0-9]{2}$'),
    description VARCHAR(255) NOT NULL,
    priority_rank SMALLINT NOT NULL CHECK (priority_rank BETWEEN 1 AND 4)
);

CREATE INDEX IF NOT EXISTS idx_aa_run ON adverse_action_reasons(scoring_run_id);

CREATE TABLE IF NOT EXISTS decisions (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    scoring_run_id UUID NOT NULL REFERENCES scoring_runs(id),
    outcome VARCHAR(50) NOT NULL CHECK (outcome IN ('APPROVED', 'DECLINED', 'REFERRED')),
    approved_limit_cents BIGINT NOT NULL DEFAULT 0 CHECK (approved_limit_cents >= 0),
    approved_apr_bps INTEGER NOT NULL DEFAULT 0 CHECK (approved_apr_bps >= 0),
    decision_type VARCHAR(50) NOT NULL CHECK (decision_type IN ('AUTOMATED', 'MANUAL_OVERRIDE')),
    underwriter_id UUID REFERENCES users(id),
    decision_notes TEXT,
    decisioned_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_decisions_app ON decisions(application_id);

CREATE TABLE IF NOT EXISTS audit_ledger (
    id UUID PRIMARY KEY,
    -- Nullable: platform-wide events such as drift measurements are ledgered too.
    application_id UUID REFERENCES credit_applications(id),
    event_type VARCHAR(100) NOT NULL,
    actor VARCHAR(100) NOT NULL,
    payload_sha256 CHAR(64) NOT NULL,
    previous_entry_hash CHAR(64) NOT NULL,
    entry_hash CHAR(64) NOT NULL UNIQUE,
    metadata JSONB NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_ledger_app ON audit_ledger(application_id);
CREATE INDEX IF NOT EXISTS idx_audit_ledger_time ON audit_ledger(recorded_at);

-- The live projection of the application event stream, written by the ingest
-- consumer. audit_ledger stays the tamper-evident record; this table exists so
-- the stream has a destination that can be queried without a Kafka client.
CREATE TABLE IF NOT EXISTS application_events (
    kafka_offset BIGINT NOT NULL,
    kafka_partition INTEGER NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    application_id UUID NOT NULL,
    actor VARCHAR(128) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    ledger_entry_hash CHAR(64) NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    consumed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- offset and partition are reserved words in Postgres, so the columns carry
    -- the kafka_ prefix rather than quoting them in every query.
    PRIMARY KEY (kafka_partition, kafka_offset)
);

CREATE INDEX IF NOT EXISTS idx_application_events_app ON application_events(application_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_application_events_type ON application_events(event_type, occurred_at DESC);

CREATE TABLE IF NOT EXISTS drift_metrics (
    id UUID PRIMARY KEY,
    model_version_id UUID NOT NULL REFERENCES model_versions(id),
    feature_name VARCHAR(100) NOT NULL,
    psi_value NUMERIC(6, 4) NOT NULL CHECK (psi_value >= 0),
    sample_size INTEGER NOT NULL CHECK (sample_size > 0),
    status VARCHAR(20) NOT NULL CHECK (status IN ('STABLE', 'MODERATE_DRIFT', 'CRITICAL_DRIFT')),
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_drift_model ON drift_metrics(model_version_id, evaluated_at DESC);

-- The ledger is append-only at the database level, not by convention.
CREATE OR REPLACE FUNCTION forbid_ledger_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_ledger is append-only: % is not permitted', TG_OP;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_ledger_append_only ON audit_ledger;
CREATE TRIGGER audit_ledger_append_only
    BEFORE UPDATE OR DELETE ON audit_ledger
    FOR EACH ROW EXECUTE FUNCTION forbid_ledger_mutation();

CREATE OR REPLACE FUNCTION touch_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS credit_apps_touch ON credit_applications;
CREATE TRIGGER credit_apps_touch
    BEFORE UPDATE ON credit_applications
    FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
