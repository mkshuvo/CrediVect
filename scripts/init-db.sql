CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS applicants (
    id UUID PRIMARY KEY,
    external_id VARCHAR(100) UNIQUE NOT NULL,
    ssn_blind_index VARCHAR(64) UNIQUE NOT NULL,
    ssn_encrypted BYTEA NOT NULL,
    first_name VARCHAR(100) NOT NULL,
    last_name VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL,
    date_of_birth DATE NOT NULL,
    monthly_income_cents BIGINT NOT NULL CHECK (monthly_income_cents >= 0),
    currency_code CHAR(3) NOT NULL DEFAULT 'USD',
    employment_status VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS credit_applications (
    id UUID PRIMARY KEY,
    applicant_id UUID NOT NULL REFERENCES applicants(id),
    requested_amount_cents BIGINT NOT NULL CHECK (requested_amount_cents > 0),
    currency_code CHAR(3) NOT NULL DEFAULT 'USD',
    loan_term_months INTEGER NOT NULL CHECK (loan_term_months BETWEEN 6 AND 84),
    loan_purpose VARCHAR(100) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'SUBMITTED',
    workflow_id VARCHAR(128) NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_credit_apps_applicant ON credit_applications(applicant_id);
CREATE INDEX IF NOT EXISTS idx_credit_apps_status ON credit_applications(status);

CREATE TABLE IF NOT EXISTS bureau_reports (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    bureau_name VARCHAR(50) NOT NULL,
    raw_payload JSONB NOT NULL,
    bureau_score SMALLINT NOT NULL CHECK (bureau_score BETWEEN 300 AND 850),
    total_debt_cents BIGINT NOT NULL CHECK (total_debt_cents >= 0),
    revolving_balance_cents BIGINT NOT NULL CHECK (revolving_balance_cents >= 0),
    revolving_limit_cents BIGINT NOT NULL CHECK (revolving_limit_cents >= 0),
    revolving_utilization_bps INTEGER NOT NULL CHECK (revolving_utilization_bps >= 0),
    delinquencies_24m SMALLINT NOT NULL DEFAULT 0,
    inquiries_6m SMALLINT NOT NULL DEFAULT 0,
    total_trades SMALLINT NOT NULL DEFAULT 0,
    open_trades SMALLINT NOT NULL DEFAULT 0,
    bankruptcy_flag BOOLEAN NOT NULL DEFAULT FALSE,
    valid_time TIMESTAMPTZ NOT NULL,
    transaction_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bureau_reports_app ON bureau_reports(application_id);

CREATE TABLE IF NOT EXISTS feature_vectors (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    dti_bps INTEGER NOT NULL,
    revolving_utilization_bps INTEGER NOT NULL,
    delinquencies_24m SMALLINT NOT NULL,
    inquiries_6m SMALLINT NOT NULL,
    age_of_oldest_trade_months INTEGER NOT NULL,
    income_to_loan_bps INTEGER NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

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
    risk_tier VARCHAR(50) NOT NULL,
    execution_time_micros INTEGER NOT NULL,
    scored_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS shap_explanations (
    id UUID PRIMARY KEY,
    scoring_run_id UUID NOT NULL REFERENCES scoring_runs(id),
    feature_name VARCHAR(100) NOT NULL,
    feature_value NUMERIC(14, 4) NOT NULL,
    shap_value NUMERIC(10, 6) NOT NULL,
    rank SMALLINT NOT NULL
);

CREATE TABLE IF NOT EXISTS adverse_action_reasons (
    id UUID PRIMARY KEY,
    scoring_run_id UUID NOT NULL REFERENCES scoring_runs(id),
    reason_code CHAR(4) NOT NULL,
    description VARCHAR(255) NOT NULL,
    priority_rank SMALLINT NOT NULL CHECK (priority_rank BETWEEN 1 AND 4)
);

CREATE TABLE IF NOT EXISTS decisions (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    scoring_run_id UUID NOT NULL REFERENCES scoring_runs(id),
    outcome VARCHAR(50) NOT NULL,
    approved_limit_cents BIGINT NOT NULL DEFAULT 0,
    approved_apr_bps INTEGER NOT NULL DEFAULT 0,
    decision_type VARCHAR(50) NOT NULL,
    underwriter_id UUID,
    decision_notes TEXT,
    decisioned_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_ledger (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES credit_applications(id),
    event_type VARCHAR(100) NOT NULL,
    actor VARCHAR(100) NOT NULL,
    payload_sha256 CHAR(64) NOT NULL,
    previous_entry_hash CHAR(64) NOT NULL,
    entry_hash CHAR(64) NOT NULL,
    metadata JSONB NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_ledger_app ON audit_ledger(application_id);
CREATE INDEX IF NOT EXISTS idx_audit_ledger_time ON audit_ledger(recorded_at);

INSERT INTO model_versions (id, model_name, version, algorithm, artifact_path, dataset_sha256, is_active)
VALUES (
    '01918a2b-4c5d-7000-8000-000000000001',
    'CrediVect-DualEngine',
    'v1.0.0',
    'WOE_LIGHTGBM_ENSEMBLE',
    'services/scoring/engine/models/model.txt',
    'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
    TRUE
) ON CONFLICT DO NOTHING;
