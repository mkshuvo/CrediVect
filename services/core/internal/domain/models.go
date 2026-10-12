package domain

import (
	"time"
)

type ApplicationStatus string

const (
	StatusSubmitted ApplicationStatus = "SUBMITTED"
	StatusInReview  ApplicationStatus = "IN_REVIEW"
	StatusApproved  ApplicationStatus = "APPROVED"
	StatusDeclined  ApplicationStatus = "DECLINED"
	StatusCancelled ApplicationStatus = "CANCELLED"
)

type Applicant struct {
	ID                 string    `json:"id"`
	ExternalID         string    `json:"external_id"`
	SSNBlindIndex      string    `json:"-"`
	FirstName          string    `json:"first_name"`
	LastName           string    `json:"last_name"`
	Email              string    `json:"email"`
	DateOfBirth        string    `json:"date_of_birth"`
	MonthlyIncomeCents int64     `json:"monthly_income_cents"` // Integer Cents
	CurrencyCode       string    `json:"currency_code"`
	EmploymentStatus   string    `json:"employment_status"`
	CreatedAt          time.Time `json:"created_at"`
}

type CreditApplication struct {
	ID                   string            `json:"id"` // UUIDv7
	ApplicantID          string            `json:"applicant_id"`
	Applicant            *Applicant        `json:"applicant,omitempty"`
	RequestedAmountCents int64             `json:"requested_amount_cents"`
	CurrencyCode         string            `json:"currency_code"`
	LoanTermMonths       int32             `json:"loan_term_months"`
	LoanPurpose          string            `json:"loan_purpose"`
	Status               ApplicationStatus `json:"status"`
	WorkflowID           string            `json:"workflow_id"`
	Version              int64             `json:"version"` // OCC
	BureauReport         *BureauReport     `json:"bureau_report,omitempty"`
	ScoringResult        *ScoringResult    `json:"scoring_result,omitempty"`
	Decision             *Decision         `json:"decision,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

type BureauReport struct {
	ID                      string    `json:"id"`
	ApplicationID           string    `json:"application_id"`
	BureauName              string    `json:"bureau_name"`
	BureauScore             int32     `json:"bureau_score"` // 300 - 850
	TotalDebtCents          int64     `json:"total_debt_cents"`
	RevolvingBalanceCents   int64     `json:"revolving_balance_cents"`
	RevolvingLimitCents     int64     `json:"revolving_limit_cents"`
	RevolvingUtilizationBps int32     `json:"revolving_utilization_bps"` // Basis Points
	Delinquencies24m        int32     `json:"delinquencies_24m"`
	Inquiries6m             int32     `json:"inquiries_6m"`
	TotalTrades             int32     `json:"total_trades"`
	OpenTrades              int32     `json:"open_trades"`
	BankruptcyFlag          bool      `json:"bankruptcy_flag"`
	AgeOfOldestTradeMonths  int32     `json:"age_of_oldest_trade_months"`
	RawPayload              string    `json:"raw_payload,omitempty"`
	ValidTime               time.Time `json:"valid_time"`
	TransactionTime         time.Time `json:"transaction_time"`
}

type FeatureVector struct {
	ID                      string    `json:"id"`
	ApplicationID           string    `json:"application_id"`
	DTIBps                  int32     `json:"dti_bps"`
	RevolvingUtilizationBps int32     `json:"revolving_utilization_bps"`
	Delinquencies24m        int32     `json:"delinquencies_24m"`
	Inquiries6m             int32     `json:"inquiries_6m"`
	AgeOfOldestTradeMonths  int32     `json:"age_of_oldest_trade_months"`
	IncomeToLoanBps         int32     `json:"income_to_loan_bps"`
	ComputedAt              time.Time `json:"computed_at"`
}

type FeatureAttribution struct {
	FeatureName  string  `json:"feature_name"`
	FeatureValue string  `json:"feature_value"`
	ShapValue    float64 `json:"shap_value"`
	Rank         int32   `json:"rank"`
}

type AdverseActionReason struct {
	ReasonCode   string `json:"reason_code"` // e.g. AA01
	Description  string `json:"description"`
	PriorityRank int32  `json:"priority_rank"`
}

type ScoringResult struct {
	ID                    string                `json:"id,omitempty"` // scoring_runs PK, UUIDv7
	ApplicationID         string                `json:"application_id,omitempty"`
	ModelVersionID        string                `json:"model_version_id,omitempty"`
	ScorecardPoints       int32                 `json:"scorecard_points"`
	CalibratedPD          string                `json:"calibrated_pd"`
	PDBasisPoints         int32                 `json:"pd_basis_points"`
	RawMLLogit            float64               `json:"raw_ml_logit"`
	RiskTier              string                `json:"risk_tier"`
	RecommendedOutcome    string                `json:"recommended_outcome"`
	MaxApprovedLimitCents int64                 `json:"max_approved_limit_cents"`
	RecommendedAPRBps     int32                 `json:"recommended_apr_bps"`
	FeatureAttributions   []FeatureAttribution  `json:"feature_attributions"`
	AdverseActionReasons  []AdverseActionReason `json:"adverse_action_reasons"`
	ExecutionTimeMicros   int64                 `json:"execution_time_micros"`
	ScoredAt              time.Time             `json:"scored_at"`
	ModelVersion          string                `json:"model_version"`
}

type Decision struct {
	ID                 string    `json:"id"`
	ApplicationID      string    `json:"application_id"`
	ScoringRunID       string    `json:"scoring_run_id,omitempty"`
	Outcome            string    `json:"outcome"` // APPROVED, DECLINED, REFERRED
	ApprovedLimitCents int64     `json:"approved_limit_cents"`
	ApprovedAPRBps     int32     `json:"approved_apr_bps"`
	DecisionType       string    `json:"decision_type"` // AUTOMATED, MANUAL_OVERRIDE
	UnderwriterID      string    `json:"underwriter_id,omitempty"`
	DecisionNotes      string    `json:"decision_notes,omitempty"`
	DecisionedAt       time.Time `json:"decisioned_at"`
}

type AuditLedgerEntry struct {
	ID                string    `json:"id"`
	ApplicationID     string    `json:"application_id"`
	EventType         string    `json:"event_type"`
	Actor             string    `json:"actor"`
	PayloadSHA256     string    `json:"payload_sha256"`
	PreviousEntryHash string    `json:"previous_entry_hash"`
	EntryHash         string    `json:"entry_hash"` // SHA256(prev_hash + payload_hash + timestamp)
	Metadata          string    `json:"metadata"`
	RecordedAt        time.Time `json:"recorded_at"`
}

type ModelVersion struct {
	ID            string    `json:"id"`
	ModelName     string    `json:"model_name"`
	Version       string    `json:"version"`
	Algorithm     string    `json:"algorithm"`
	ArtifactPath  string    `json:"artifact_path"`
	DatasetSHA256 string    `json:"dataset_sha256"`
	IsActive      bool      `json:"is_active"`
	DeployedAt    time.Time `json:"deployed_at"`
}

type DriftMetric struct {
	ID             string    `json:"id"`
	ModelVersionID string    `json:"model_version_id"`
	ModelVersion   string    `json:"model_version"`
	FeatureName    string    `json:"feature_name"`
	PSIValue       float64   `json:"psi_value"`
	SampleSize     int32     `json:"sample_size"`
	Status         string    `json:"status"`
	WindowStart    time.Time `json:"window_start"`
	WindowEnd      time.Time `json:"window_end"`
	EvaluatedAt    time.Time `json:"evaluated_at"`
}

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	FullName     string    `json:"full_name"`
	Role         string    `json:"role"` // BORROWER, UNDERWRITER, ADMIN
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type ChainVerification struct {
	Verified           bool      `json:"verified"`
	EntriesChecked     int64     `json:"entries_checked"`
	HeadHash           string    `json:"head_hash"`
	Recomputed         string    `json:"-"`
	BrokenAtEntryID    string    `json:"broken_at_entry_id,omitempty"`
	BrokenAtRecordedAt time.Time `json:"broken_at_recorded_at,omitempty"`
	Reason             string    `json:"reason,omitempty"`
}
