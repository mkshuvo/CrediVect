package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"credivect/core/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository is the only writer of CrediVect's relational state. Everything the
// underwriter queue, the status page and the governance view show is read back
// from these tables, so a restart never loses an application.
type Repository struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, url string, maxConns int32) (*Repository, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse POSTGRES_URL: %w", err)
	}
	cfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Close() { r.pool.Close() }

func (r *Repository) Pool() *pgxpool.Pool { return r.pool }

var ErrVersionConflict = errors.New("optimistic concurrency conflict")

func (r *Repository) InsertApplicant(ctx context.Context, a domain.Applicant, blindIndex, encryptedSSN string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO applicants (id, external_id, ssn_blind_index, ssn_encrypted, first_name, last_name,
		                        email, date_of_birth, monthly_income_cents, currency_code, employment_status, created_at)
		VALUES ($1,$2,$3,$4::bytea,$5,$6,$7,$8::date,$9,$10,$11,$12)
		ON CONFLICT (id) DO NOTHING`,
		a.ID, a.ExternalID, blindIndex, encryptedSSN, a.FirstName, a.LastName, a.Email,
		a.DateOfBirth, a.MonthlyIncomeCents, a.CurrencyCode, a.EmploymentStatus, a.CreatedAt)
	return err
}

func (r *Repository) InsertApplication(ctx context.Context, app domain.CreditApplication) error {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO credit_applications (id, applicant_id, requested_amount_cents, currency_code,
		                                 loan_term_months, loan_purpose, status, workflow_id, version, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO NOTHING`,
		app.ID, app.ApplicantID, app.RequestedAmountCents, app.CurrencyCode, app.LoanTermMonths,
		app.LoanPurpose, string(app.Status), app.WorkflowID, app.Version, app.CreatedAt, app.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("application %s already exists", app.ID)
	}
	return nil
}

func (r *Repository) InsertBureauReport(ctx context.Context, b domain.BureauReport) error {
	payload := b.RawPayload
	if payload == "" {
		payload = "{}"
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO bureau_reports (id, application_id, bureau_name, raw_payload, bureau_score,
		                            total_debt_cents, revolving_balance_cents, revolving_limit_cents,
		                            revolving_utilization_bps, delinquencies_24m, inquiries_6m,
		                            total_trades, open_trades, bankruptcy_flag, age_of_oldest_trade_months,
		                            valid_time, transaction_time)
		VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (id) DO NOTHING`,
		b.ID, b.ApplicationID, b.BureauName, payload, b.BureauScore, b.TotalDebtCents,
		b.RevolvingBalanceCents, b.RevolvingLimitCents, b.RevolvingUtilizationBps,
		b.Delinquencies24m, b.Inquiries6m, b.TotalTrades, b.OpenTrades, b.BankruptcyFlag,
		b.AgeOfOldestTradeMonths, b.ValidTime, b.TransactionTime)
	return err
}

func (r *Repository) InsertFeatureVector(ctx context.Context, fv domain.FeatureVector) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO feature_vectors (id, application_id, dti_bps, revolving_utilization_bps,
		                             delinquencies_24m, inquiries_6m, age_of_oldest_trade_months,
		                             income_to_loan_bps, computed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (id) DO NOTHING`,
		fv.ID, fv.ApplicationID, fv.DTIBps, fv.RevolvingUtilizationBps, fv.Delinquencies24m,
		fv.Inquiries6m, fv.AgeOfOldestTradeMonths, fv.IncomeToLoanBps, fv.ComputedAt)
	return err
}

// InsertScoringRun persists the engine output and its TreeSHAP attributions and
// adverse action reasons in one transaction, then stamps the generated run id
// back onto the result so the following decision can reference it.
func (r *Repository) InsertScoringRun(ctx context.Context, appID, modelVersionID string, res *domain.ScoringResult) error {
	runID := domain.NewUUIDv7()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO scoring_runs (id, application_id, model_version_id, scorecard_points, raw_ml_logit,
		                          calibrated_pd, pd_basis_points, risk_tier, recommended_outcome,
		                          max_approved_limit_cents, recommended_apr_bps, execution_time_micros, scored_at)
		VALUES ($1,$2,$3,$4,$5,$6::numeric,$7,$8,$9,$10,$11,$12,$13)`,
		runID, appID, modelVersionID, res.ScorecardPoints, res.RawMLLogit, res.CalibratedPD,
		res.PDBasisPoints, res.RiskTier, res.RecommendedOutcome, res.MaxApprovedLimitCents,
		res.RecommendedAPRBps, res.ExecutionTimeMicros, time.Now().UTC()); err != nil {
		return fmt.Errorf("insert scoring run: %w", err)
	}

	for _, att := range res.FeatureAttributions {
		if _, err := tx.Exec(ctx, `
			INSERT INTO shap_explanations (id, scoring_run_id, feature_name, feature_value, shap_value, rank)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			domain.NewUUIDv7(), runID, att.FeatureName, att.FeatureValue, att.ShapValue, att.Rank); err != nil {
			return fmt.Errorf("insert shap explanation: %w", err)
		}
	}

	for _, reason := range res.AdverseActionReasons {
		if _, err := tx.Exec(ctx, `
			INSERT INTO adverse_action_reasons (id, scoring_run_id, reason_code, description, priority_rank)
			VALUES ($1,$2,$3,$4,$5)`,
			domain.NewUUIDv7(), runID, reason.ReasonCode, reason.Description, reason.PriorityRank); err != nil {
			return fmt.Errorf("insert adverse action reason: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	res.ID = runID
	res.ApplicationID = appID
	res.ModelVersionID = modelVersionID
	return nil
}

func (r *Repository) InsertDecision(ctx context.Context, d domain.Decision) error {
	var underwriter any
	if d.UnderwriterID != "" {
		underwriter = d.UnderwriterID
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO decisions (id, application_id, scoring_run_id, outcome, approved_limit_cents,
		                       approved_apr_bps, decision_type, underwriter_id, decision_notes, decisioned_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO NOTHING`,
		d.ID, d.ApplicationID, d.ScoringRunID, d.Outcome, d.ApprovedLimitCents, d.ApprovedAPRBps,
		d.DecisionType, underwriter, d.DecisionNotes, d.DecisionedAt)
	return err
}

// UpdateApplicationStatus performs a compare-and-swap on version so two
// underwriters cannot silently clobber each other's adjudication.
func (r *Repository) UpdateApplicationStatus(ctx context.Context, appID string, status domain.ApplicationStatus, expectedVersion int64, updatedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE credit_applications
		SET status = $2, version = version + 1, updated_at = $3
		WHERE id = $1 AND version = $4`,
		appID, string(status), updatedAt, expectedVersion)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrVersionConflict
	}
	return nil
}

// appSelect reads one application with its applicant, newest bureau report,
// newest scoring run and newest decision. The joined columns are COALESCE'd so
// an unreviewed or unscored row scans into plain Go types instead of a null
// wrapper per column.
const appSelect = `
	SELECT a.id::text, a.applicant_id::text, a.requested_amount_cents, a.currency_code, a.loan_term_months,
	       a.loan_purpose, a.status, a.workflow_id, a.version, a.created_at, a.updated_at,
	       p.id::text, p.external_id, p.ssn_blind_index, p.first_name, p.last_name, p.email, to_char(p.date_of_birth, 'YYYY-MM-DD'),
	       p.monthly_income_cents, p.currency_code, p.employment_status, p.created_at,
	       COALESCE(b.id::text, ''), COALESCE(b.bureau_name, ''), COALESCE(b.bureau_score, 0),
	       COALESCE(b.total_debt_cents, 0), COALESCE(b.revolving_balance_cents, 0),
	       COALESCE(b.revolving_limit_cents, 0), COALESCE(b.revolving_utilization_bps, 0),
	       COALESCE(b.delinquencies_24m, 0), COALESCE(b.inquiries_6m, 0),
	       COALESCE(b.total_trades, 0), COALESCE(b.open_trades, 0), COALESCE(b.bankruptcy_flag, FALSE),
	       COALESCE(b.age_of_oldest_trade_months, 0), COALESCE(b.valid_time, TIMESTAMPTZ 'epoch'),
	       COALESCE(b.transaction_time, TIMESTAMPTZ 'epoch'),
	       COALESCE(s.id::text, ''), COALESCE(s.scorecard_points, 0), COALESCE(s.calibrated_pd::text, ''),
	       COALESCE(s.pd_basis_points, 0), COALESCE(s.risk_tier, ''),
	       COALESCE(s.recommended_outcome, ''), COALESCE(s.max_approved_limit_cents, 0),
	       COALESCE(s.recommended_apr_bps, 0), COALESCE(s.execution_time_micros, 0),
	       COALESCE(s.raw_ml_logit, 0), COALESCE(s.scored_at, TIMESTAMPTZ 'epoch'),
	       COALESCE(mv.version, ''), COALESCE(mv.id::text, ''),
	       COALESCE(d.id::text, ''), COALESCE(d.outcome, ''), COALESCE(d.approved_limit_cents, 0),
	       COALESCE(d.approved_apr_bps, 0), COALESCE(d.decision_type, ''),
	       COALESCE(d.underwriter_id::text, ''), COALESCE(d.decision_notes, ''), COALESCE(d.decisioned_at, TIMESTAMPTZ 'epoch')
	FROM credit_applications a
	JOIN applicants p ON p.id = a.applicant_id
	LEFT JOIN LATERAL (SELECT * FROM bureau_reports r WHERE r.application_id = a.id ORDER BY r.transaction_time DESC LIMIT 1) b ON TRUE
	LEFT JOIN LATERAL (SELECT * FROM scoring_runs sr WHERE sr.application_id = a.id ORDER BY sr.scored_at DESC LIMIT 1) s ON TRUE
	LEFT JOIN model_versions mv ON mv.id = s.model_version_id
	LEFT JOIN LATERAL (SELECT * FROM decisions dc WHERE dc.application_id = a.id ORDER BY dc.decisioned_at DESC LIMIT 1) d ON TRUE`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanApplication(sc rowScanner) (domain.CreditApplication, error) {
	var app domain.CreditApplication
	var applicant domain.Applicant
	var bureau domain.BureauReport
	var run domain.ScoringResult
	var decision domain.Decision
	var runID, modelID, modelVer, decisionID string

	err := sc.Scan(
		&app.ID, &app.ApplicantID, &app.RequestedAmountCents, &app.CurrencyCode, &app.LoanTermMonths,
		&app.LoanPurpose, &app.Status, &app.WorkflowID, &app.Version, &app.CreatedAt, &app.UpdatedAt,
		&applicant.ID, &applicant.ExternalID, &applicant.SSNBlindIndex, &applicant.FirstName, &applicant.LastName,
		&applicant.Email, &applicant.DateOfBirth, &applicant.MonthlyIncomeCents,
		&applicant.CurrencyCode, &applicant.EmploymentStatus, &applicant.CreatedAt,
		&bureau.ID, &bureau.BureauName, &bureau.BureauScore, &bureau.TotalDebtCents,
		&bureau.RevolvingBalanceCents, &bureau.RevolvingLimitCents, &bureau.RevolvingUtilizationBps,
		&bureau.Delinquencies24m, &bureau.Inquiries6m, &bureau.TotalTrades,
		&bureau.OpenTrades, &bureau.BankruptcyFlag, &bureau.AgeOfOldestTradeMonths,
		&bureau.ValidTime, &bureau.TransactionTime,
		&runID, &run.ScorecardPoints, &run.CalibratedPD, &run.PDBasisPoints, &run.RiskTier,
		&run.RecommendedOutcome, &run.MaxApprovedLimitCents, &run.RecommendedAPRBps,
		&run.ExecutionTimeMicros, &run.RawMLLogit, &run.ScoredAt, &modelVer, &modelID,
		&decisionID, &decision.Outcome, &decision.ApprovedLimitCents, &decision.ApprovedAPRBps,
		&decision.DecisionType, &decision.UnderwriterID, &decision.DecisionNotes, &decision.DecisionedAt,
	)
	if err != nil {
		return app, err
	}

	app.Applicant = &applicant
	if bureau.ID != "" {
		bureau.ApplicationID = app.ID
		app.BureauReport = &bureau
	}
	if runID != "" {
		run.ID = runID
		run.ApplicationID = app.ID
		run.ModelVersionID = modelID
		run.ModelVersion = modelVer
		app.ScoringResult = &run
	}
	if decisionID != "" {
		decision.ID = decisionID
		decision.ApplicationID = app.ID
		if app.ScoringResult != nil {
			decision.ScoringRunID = app.ScoringResult.ID
		}
		app.Decision = &decision
	}
	return app, nil
}

func (r *Repository) GetApplication(ctx context.Context, id string) (domain.CreditApplication, error) {
	row := r.pool.QueryRow(ctx, appSelect+` WHERE a.id = $1`, id)
	app, err := scanApplication(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CreditApplication{}, fmt.Errorf("application %s not found", id)
	}
	if err != nil {
		return domain.CreditApplication{}, err
	}
	if err := r.attachExplanation(ctx, &app); err != nil {
		return app, err
	}
	return app, nil
}

func (r *Repository) ListApplications(ctx context.Context, status, tier string, limit, offset int) ([]domain.CreditApplication, error) {
	where := ""
	args := []any{}
	switch {
	case status != "" && tier != "":
		where = "WHERE a.status = $1 AND COALESCE(s.risk_tier, '') = $2"
		args = []any{status, tier}
	case status != "":
		where = "WHERE a.status = $1"
		args = []any{status}
	case tier != "":
		where = "WHERE COALESCE(s.risk_tier, '') = $1"
		args = []any{tier}
	}
	if where != "" {
		where = " " + where
	}
	args = append(args, limit, offset)
	sql := appSelect + where + fmt.Sprintf(" ORDER BY a.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	apps := []domain.CreditApplication{}
	runs := []string{}
	for rows.Next() {
		app, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		if app.ScoringResult != nil && app.ScoringResult.ID != "" {
			runs = append(runs, app.ScoringResult.ID)
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return r.attachBatchExplanation(ctx, apps, runs)
}

func (r *Repository) attachExplanation(ctx context.Context, app *domain.CreditApplication) error {
	runIDs := []string{}
	if app.ScoringResult != nil && app.ScoringResult.ID != "" {
		runIDs = []string{app.ScoringResult.ID}
	}
	apps, err := r.attachBatchExplanation(ctx, []domain.CreditApplication{*app}, runIDs)
	if err != nil {
		return err
	}
	*app = apps[0]
	return nil
}

// attachBatchExplanation loads the TreeSHAP attributions and FCRA reason codes
// for every scoring run in one round trip per table.
func (r *Repository) attachBatchExplanation(ctx context.Context, apps []domain.CreditApplication, runIDs []string) ([]domain.CreditApplication, error) {
	if len(runIDs) == 0 || len(apps) == 0 {
		return apps, nil
	}
	byRun := map[string]*domain.ScoringResult{}
	for i := range apps {
		if apps[i].ScoringResult != nil && apps[i].ScoringResult.ID != "" {
			byRun[apps[i].ScoringResult.ID] = apps[i].ScoringResult
		}
	}

	rows, err := r.pool.Query(ctx, `
		SELECT scoring_run_id::text, feature_name, feature_value, shap_value, rank
		FROM shap_explanations WHERE scoring_run_id = ANY($1::uuid[]) ORDER BY rank`, runIDs)
	if err != nil {
		return nil, fmt.Errorf("load shap explanations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var runID, name, value string
		var shap float64
		var rank int32
		if err := rows.Scan(&runID, &name, &value, &shap, &rank); err != nil {
			return nil, err
		}
		if res := byRun[runID]; res != nil {
			res.FeatureAttributions = append(res.FeatureAttributions, domain.FeatureAttribution{
				FeatureName: name, FeatureValue: value, ShapValue: shap, Rank: rank,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows2, err := r.pool.Query(ctx, `
		SELECT scoring_run_id::text, reason_code, description, priority_rank
		FROM adverse_action_reasons WHERE scoring_run_id = ANY($1::uuid[]) ORDER BY priority_rank`, runIDs)
	if err != nil {
		return nil, fmt.Errorf("load adverse action reasons: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var runID, code, desc string
		var rank int32
		if err := rows2.Scan(&runID, &code, &desc, &rank); err != nil {
			return nil, err
		}
		if res := byRun[runID]; res != nil {
			res.AdverseActionReasons = append(res.AdverseActionReasons, domain.AdverseActionReason{
				ReasonCode: code, Description: desc, PriorityRank: rank,
			})
		}
	}
	return apps, rows2.Err()
}

func (r *Repository) ActiveModelVersion(ctx context.Context) (domain.ModelVersion, error) {
	var mv domain.ModelVersion
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, model_name, version, algorithm, artifact_path, dataset_sha256, is_active, deployed_at
		FROM model_versions WHERE is_active ORDER BY deployed_at DESC LIMIT 1`).
		Scan(&mv.ID, &mv.ModelName, &mv.Version, &mv.Algorithm, &mv.ArtifactPath, &mv.DatasetSHA256, &mv.IsActive, &mv.DeployedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return mv, errors.New("no active model version registered; run make seed or scripts/register-model.py")
	}
	return mv, err
}

func (r *Repository) UpsertModelVersion(ctx context.Context, mv domain.ModelVersion) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO model_versions (id, model_name, version, algorithm, artifact_path, dataset_sha256, is_active, deployed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (id) DO UPDATE SET model_name = EXCLUDED.model_name, version = EXCLUDED.version,
		                               algorithm = EXCLUDED.algorithm, artifact_path = EXCLUDED.artifact_path,
		                               dataset_sha256 = EXCLUDED.dataset_sha256, is_active = EXCLUDED.is_active,
		                               deployed_at = EXCLUDED.deployed_at`,
		mv.ID, mv.ModelName, mv.Version, mv.Algorithm, mv.ArtifactPath, mv.DatasetSHA256, mv.IsActive, mv.DeployedAt)
	return err
}

func (r *Repository) RecentPDs(ctx context.Context, modelVersionID string, limit int) ([]float64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT calibrated_pd::float8 FROM scoring_runs
		WHERE model_version_id = $1 ORDER BY scored_at DESC LIMIT $2`, modelVersionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []float64{}
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) InsertDriftMetric(ctx context.Context, d domain.DriftMetric) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO drift_metrics (id, model_version_id, feature_name, psi_value, sample_size, status, window_start, window_end, evaluated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (id) DO NOTHING`,
		d.ID, d.ModelVersionID, d.FeatureName, d.PSIValue, d.SampleSize, d.Status,
		d.WindowStart, d.WindowEnd, d.EvaluatedAt)
	return err
}

func (r *Repository) ListDriftMetrics(ctx context.Context, limit int) ([]domain.DriftMetric, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id::text, d.model_version_id::text, d.feature_name, d.psi_value, d.sample_size, d.status,
		       d.window_start, d.window_end, d.evaluated_at, mv.version
		FROM drift_metrics d JOIN model_versions mv ON mv.id = d.model_version_id
		ORDER BY d.evaluated_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.DriftMetric{}
	for rows.Next() {
		var d domain.DriftMetric
		if err := rows.Scan(&d.ID, &d.ModelVersionID, &d.FeatureName, &d.PSIValue, &d.SampleSize,
			&d.Status, &d.WindowStart, &d.WindowEnd, &d.EvaluatedAt, &d.ModelVersion); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) CountApplicationsByStatus(ctx context.Context) (map[string]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT status, count(*) FROM credit_applications GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var s string
		var n int64
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		counts[s] = n
	}
	return counts, rows.Err()
}

// LatencyReport backs the published sub-15ms scoring SLA claim with measured
// percentiles taken from persisted scoring runs.
type LatencyReport struct {
	Samples         int       `json:"samples"`
	P50Micros       float64   `json:"p50_micros"`
	P95Micros       float64   `json:"p95_micros"`
	P99Micros       float64   `json:"p99_micros"`
	MaxMicros       float64   `json:"max_micros"`
	MeanMicros      float64   `json:"mean_micros"`
	Within15msShare float64   `json:"share_within_15ms"`
	WindowStartedAt time.Time `json:"window_started_at"`
}

func (r *Repository) LatencyReport(ctx context.Context, modelVersionID string, limit int) (LatencyReport, error) {
	var out LatencyReport
	var start *time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT count(*),
		       percentile_cont(0.5)  WITHIN GROUP (ORDER BY execution_time_micros),
		       percentile_cont(0.95) WITHIN GROUP (ORDER BY execution_time_micros),
		       percentile_cont(0.99) WITHIN GROUP (ORDER BY execution_time_micros),
		       max(execution_time_micros),
		       avg(execution_time_micros),
		       avg(CASE WHEN execution_time_micros <= 15000 THEN 1.0 ELSE 0.0 END),
		       min(scored_at)
		FROM (SELECT execution_time_micros, scored_at FROM scoring_runs
		      WHERE model_version_id = $1 ORDER BY scored_at DESC LIMIT $2) t`,
		modelVersionID, limit).
		Scan(&out.Samples, &out.P50Micros, &out.P95Micros, &out.P99Micros, &out.MaxMicros,
			&out.MeanMicros, &out.Within15msShare, &start)
	if err != nil {
		return out, err
	}
	if start != nil {
		out.WindowStartedAt = *start
	}
	return out, nil
}

func jsonMap(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
