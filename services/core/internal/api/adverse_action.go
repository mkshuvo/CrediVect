package api

import (
	"fmt"
	"net/http"
	"strings"

	"credivect/core/internal/domain"
)

// AdverseActionNotice is the FCRA 1681m disclosure assembled from persisted
// engine output: the credit file that was used, the score derived from it, and
// the ranked TreeSHAP drivers behind the decline.
type AdverseActionNotice struct {
	ApplicationID   string                       `json:"application_id"`
	DecidedAt       string                       `json:"decided_at"`
	Outcome         string                       `json:"outcome"`
	Creditor        creditorDisclosure           `json:"creditor"`
	ReportingAgency agencyDisclosure             `json:"reporting_agency"`
	ScoreUsed       scoreDisclosure              `json:"score_used"`
	Reasons         []domain.AdverseActionReason `json:"reasons"`
	KeyFactors      []domain.FeatureAttribution  `json:"key_factors"`
	ModelVersion    string                       `json:"model_version"`
	Rights          rightsDisclosure             `json:"consumer_rights"`
}

type creditorDisclosure struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
}

type agencyDisclosure struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
}

type scoreDisclosure struct {
	BureauScore     int32  `json:"bureau_score"`
	ScorecardPoints int32  `json:"scorecard_points"`
	CalibratedPD    string `json:"calibrated_pd"`
	RiskTier        string `json:"risk_tier"`
	ScoreSource     string `json:"score_source"`
}

type rightsDisclosure struct {
	FreeReportWindowDays int  `json:"free_report_window_days"`
	CanDisputeAccuracy   bool `json:"can_dispute_accuracy"`
	CanRequestReasons    bool `json:"can_request_reasons"`
}

func (s *Server) loadNotice(r *http.Request) (AdverseActionNotice, domain.CreditApplication, error) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		return AdverseActionNotice{}, domain.CreditApplication{}, fmt.Errorf("id query parameter is required")
	}
	app, err := s.repo.GetApplication(r.Context(), id)
	if err != nil {
		return AdverseActionNotice{}, app, err
	}
	if app.ScoringResult == nil {
		return AdverseActionNotice{}, app, fmt.Errorf("application %s has not been scored", id)
	}
	if app.Status != domain.StatusDeclined && app.Status != domain.StatusInReview {
		return AdverseActionNotice{}, app, fmt.Errorf("an adverse action notice applies to a declined or referred application; this one is %s", app.Status)
	}
	bureauName, bureauAddress := "Unattributed consumer reporting agency", "Address unavailable"
	if app.BureauReport != nil {
		bureauName = app.BureauReport.BureauName
		bureauAddress = s.cfg.NoticeCRAAddress
	}
	notice := AdverseActionNotice{
		ApplicationID:   app.ID,
		Outcome:         string(app.Status),
		DecidedAt:       app.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		Creditor:        creditorDisclosure{Name: s.cfg.NoticeCreditorName, Address: s.cfg.NoticeCreditorAddress, Phone: "1-800-555-0142"},
		ReportingAgency: agencyDisclosure{Name: bureauName, Address: bureauAddress, Phone: "1-800-555-0170"},
		ScoreUsed: scoreDisclosure{
			BureauScore:     bureauScoreOrZero(app),
			ScorecardPoints: app.ScoringResult.ScorecardPoints,
			CalibratedPD:    app.ScoringResult.CalibratedPD,
			RiskTier:        app.ScoringResult.RiskTier,
			ScoreSource:     "CrediVect WoE scorecard, model " + app.ScoringResult.ModelVersion,
		},
		Reasons:      app.ScoringResult.AdverseActionReasons,
		KeyFactors:   app.ScoringResult.FeatureAttributions,
		ModelVersion: app.ScoringResult.ModelVersion,
		Rights:       rightsDisclosure{FreeReportWindowDays: 60, CanDisputeAccuracy: true, CanRequestReasons: true},
	}
	if app.Decision != nil {
		notice.Outcome = app.Decision.Outcome
		notice.DecidedAt = app.Decision.DecisionedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return notice, app, nil
}

func (s *Server) HandleAdverseAction(w http.ResponseWriter, r *http.Request) {
	notice, _, err := s.loadNotice(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, notice)
}

// HandleAdverseActionNotice renders the same record as a plain-text letter the
// borrower can keep, with the statutory disclosures spelled out.
func (s *Server) HandleAdverseActionNotice(w http.ResponseWriter, r *http.Request) {
	notice, app, err := s.loadNotice(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	body := renderNotice(notice, app)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="adverse-action-%s.txt"`, notice.ApplicationID))
	_, _ = w.Write([]byte(body))
}

func renderNotice(n AdverseActionNotice, app domain.CreditApplication) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\n", n.Creditor.Name, n.Creditor.Address)
	fmt.Fprintf(&b, "ADVERSE ACTION NOTICE - Fair Credit Reporting Act 15 U.S.C. 1681m\n\n")
	fmt.Fprintf(&b, "Application reference : %s\n", n.ApplicationID)
	fmt.Fprintf(&b, "Borrower              : %s %s\n", app.Applicant.FirstName, app.Applicant.LastName)
	fmt.Fprintf(&b, "Decision              : %s on %s\n\n", n.Outcome, n.DecidedAt)

	fmt.Fprintf(&b, "1. Consumer reporting agency whose report was used\n")
	fmt.Fprintf(&b, "   %s, %s, %s\n\n", n.ReportingAgency.Name, n.ReportingAgency.Address, n.ReportingAgency.Phone)

	fmt.Fprintf(&b, "2. Credit score information\n")
	fmt.Fprintf(&b, "   Bureau score reported   : %d\n", n.ScoreUsed.BureauScore)
	fmt.Fprintf(&b, "   CrediVect scorecard     : %d (300-850)\n", n.ScoreUsed.ScorecardPoints)
	fmt.Fprintf(&b, "   Modelled probability of default: %s\n", n.ScoreUsed.CalibratedPD)
	fmt.Fprintf(&b, "   Risk tier               : %s\n", n.ScoreUsed.RiskTier)
	fmt.Fprintf(&b, "   Score source            : %s\n\n", n.ScoreUsed.ScoreSource)

	fmt.Fprintf(&b, "3. Principal reasons for this action\n")
	if len(n.Reasons) == 0 {
		b.WriteString("   (none recorded)\n")
	}
	for _, reason := range n.Reasons {
		fmt.Fprintf(&b, "   %d) [%s] %s\n", reason.PriorityRank, reason.ReasonCode, reason.Description)
	}

	fmt.Fprintf(&b, "\n4. Measurement of the listed factors (log-odds of default; positive raises risk)\n")
	for _, factor := range n.KeyFactors {
		fmt.Fprintf(&b, "   %-28s value %-10s contribution %+.6f\n", factor.FeatureName, factor.FeatureValue, factor.ShapValue)
	}

	fmt.Fprintf(&b, `
5. Your rights
   - The agency named above must give you a free copy of your report if you
     request it within 60 days of receiving this notice.
   - You may dispute directly with that agency any information you believe is
     inaccurate or incomplete.
   - You may ask us for the specific reasons behind this decision at %s.
   - This decision was reached with model version %s. Reason codes above are
     ordered by measured impact on the modelled probability of default.
`, n.Creditor.Phone, n.ModelVersion)
	return b.String()
}

func bureauScoreOrZero(app domain.CreditApplication) int32 {
	if app.BureauReport == nil {
		return 0
	}
	return app.BureauReport.BureauScore
}
