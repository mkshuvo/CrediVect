package api

import (
	"net/http"
	"strings"
	"time"

	"credivect/core/internal/domain"
	"credivect/core/internal/governance"
)

func (s *Server) HandleLedger(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 100, 1, 500)
	entries, err := s.repo.ListAudit(r.Context(), limit, strings.TrimSpace(r.URL.Query().Get("application_id")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read the audit ledger")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// HandleVerifyChain recomputes the hash chain from the genesis constant. The UI
// must not claim integrity it has not checked.
func (s *Server) HandleVerifyChain(w http.ResponseWriter, r *http.Request) {
	result, err := s.repo.VerifyChain(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "chain verification failed to run: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) HandleModelVersion(w http.ResponseWriter, r *http.Request) {
	mv, err := s.repo.ActiveModelVersion(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"model_version": mv, "served_by_engine": s.scoring.ModelVersion(),
		"consistent": mv.Version == s.scoring.ModelVersion(),
	})
}

func (s *Server) HandleLatency(w http.ResponseWriter, r *http.Request) {
	mv, err := s.repo.ActiveModelVersion(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	report, err := s.repo.LatencyReport(r.Context(), mv.ID, queryInt(r, "samples", 1000, 10, 20000))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not compute latency percentiles")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"model_version": mv.Version, "sla_target_ms": 15, "measured": report,
		"sla_met": report.P95Micros <= 15000,
	})
}

func (s *Server) HandleDriftList(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.repo.ListDriftMetrics(r.Context(), queryInt(r, "limit", 20, 1, 200))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read drift metrics")
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

// HandleDriftEvaluate measures the live PD population against the training
// baseline and stores the result, so the governance number is recomputed, never
// hardcoded.
func (s *Server) HandleDriftEvaluate(w http.ResponseWriter, r *http.Request) {
	baseline, err := governance.LoadBaseline(s.cfg.PSIBaselineFile)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	mv, err := s.repo.ActiveModelVersion(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	samples := queryInt(r, "samples", 5000, 50, 50000)
	observations, err := s.repo.RecentPDs(r.Context(), mv.ID, samples)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read scored population")
		return
	}
	result, err := baseline.PSI(observations)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	windowEnd := time.Now().UTC()
	metric := domain.DriftMetric{
		ID: domain.NewUUIDv7(), ModelVersionID: mv.ID, ModelVersion: mv.Version,
		FeatureName: result.Feature, PSIValue: result.Value, SampleSize: int32(result.SampleSize),
		Status: result.Status, WindowStart: windowEnd.Add(-72 * time.Hour), WindowEnd: windowEnd,
		EvaluatedAt: windowEnd,
	}
	if err := s.repo.InsertDriftMetric(r.Context(), metric); err != nil {
		writeError(w, http.StatusInternalServerError, "could not store the drift measurement")
		return
	}
	if _, err := s.repo.AppendAudit(r.Context(), "", "DRIFT_MEASURED", "SYSTEM", map[string]any{
		"model_version_id": mv.ID, "psi": result.Value, "status": result.Status, "sample_size": result.SampleSize,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "drift measured but the audit write failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"metric": metric, "bins": result.Bins})
}
