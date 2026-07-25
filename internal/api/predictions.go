package api

import (
	"net/http"
	"time"

	"folicular/internal/api/problem"
	"folicular/internal/auth"
	"folicular/internal/cyclecalc"
	"folicular/internal/domain"
)

// HandlePredictionsCurrent serves on-demand estimates computed from the
// account's own recorded cycle starts. Estimates are never stored as facts
// and never presented as certainties (see internal/cyclecalc and
// docs/research/03-phases-and-ovulation.md).
func (d *Deps) HandlePredictionsCurrent(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	rows, err := d.Q.ListCycleStarts(r.Context(), accountID)
	if err != nil {
		d.Log.Error("cycle starts lookup failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	starts := make([]time.Time, 0, len(rows))
	for _, s := range rows {
		if t, err := domain.ParseDate(s); err == nil {
			starts = append(starts, t)
		}
	}
	writeJSON(w, http.StatusOK, cyclecalc.Estimate(starts, time.Now()))
}
