package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"folicular/internal/api/problem"
	"folicular/internal/auth"
	"folicular/internal/db/dbgen"
	"folicular/internal/domain"
)

// Read models over validated server state. The sync endpoints remain the
// replication path; these are convenience projections.

func dateRange(w http.ResponseWriter, r *http.Request, defaultFromDays, defaultToDays int) (string, string, bool) {
	now := time.Now()
	from := now.AddDate(0, 0, defaultFromDays).Format(time.DateOnly)
	to := now.AddDate(0, 0, defaultToDays).Format(time.DateOnly)
	if v := r.URL.Query().Get("from"); v != "" {
		if _, err := domain.ParseDate(v); err != nil {
			problem.Write(w, r, problem.Status(http.StatusBadRequest, "Requête invalide", "from: date ISO invalide"))
			return "", "", false
		}
		from = v
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if _, err := domain.ParseDate(v); err != nil {
			problem.Write(w, r, problem.Status(http.StatusBadRequest, "Requête invalide", "to: date ISO invalide"))
			return "", "", false
		}
		to = v
	}
	if from > to {
		problem.Write(w, r, problem.Status(http.StatusBadRequest, "Requête invalide", "from: postérieur à to"))
		return "", "", false
	}
	return from, to, true
}

type cycleView struct {
	ID           string  `json:"id"`
	StartDate    string  `json:"start_date"`
	EndDate      *string `json:"end_date"`
	LengthDays   *int    `json:"length_days"`
	BleedingDays *int    `json:"bleeding_days"`
	Certainty    string  `json:"certainty"`
	Source       string  `json:"source"`
	Notes        string  `json:"notes"`
	UpdatedAt    string  `json:"updated_at"`
}

// HandleListCycles lists live cycles overlapping the requested range.
// length_days is derived when start and end dates are known.
func (d *Deps) HandleListCycles(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	from, to, ok := dateRange(w, r, -365, 60)
	if !ok {
		return
	}
	rows, err := d.Q.ListCyclesByRange(r.Context(), dbgen.ListCyclesByRangeParams{
		AccountID: accountID,
		StartDate: from,
		StartDate_2: to,
	})
	if err != nil {
		d.Log.Error("cycle list failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	out := make([]cycleView, 0, len(rows))
	for _, row := range rows {
		c := rowToCycle(row)
		length := c.LengthDays
		if length == nil && c.EndDate != nil {
			if start, err1 := domain.ParseDate(c.StartDate); err1 == nil {
				if end, err2 := domain.ParseDate(*c.EndDate); err2 == nil {
					l := int(end.Sub(start).Hours()/24) + 1
					length = &l
				}
			}
		}
		out = append(out, cycleView{
			ID: c.ID, StartDate: c.StartDate, EndDate: c.EndDate,
			LengthDays: length, BleedingDays: c.BleedingDays,
			Certainty: c.Certainty, Source: c.Source, Notes: c.Notes,
			UpdatedAt: c.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cycles": out})
}

// Day view ---------------------------------------------------------------

type dayView struct {
	Date        string          `json:"date"`
	CycleDay    *int            `json:"cycle_day"`
	Bleeding    *bleedingBrief  `json:"bleeding"`
	Entry       *entryBrief     `json:"entry"`
	Symptoms    []symptomBrief  `json:"symptoms"`
	Biomarkers  *biomarkerBrief `json:"biomarkers"`
	Medications []medBrief      `json:"medications"`
}

type bleedingBrief struct {
	Flow           string `json:"flow"`
	Intermenstrual bool   `json:"intermenstrual"`
	ProductCount   *int   `json:"product_count"`
}

type entryBrief struct {
	PainLevel   *int   `json:"pain_level"`
	MoodLevel   *int   `json:"mood_level"`
	EnergyLevel *int   `json:"energy_level"`
	Notes       string `json:"notes"`
}

type symptomBrief struct {
	Key      string `json:"key"`
	Severity int    `json:"severity"`
	Notes    string `json:"notes"`
}

type biomarkerBrief struct {
	BBTCelsius     *float64 `json:"bbt_celsius"`
	CervicalFluid  *string  `json:"cervical_fluid"`
	CervixPosition *string  `json:"cervix_position"`
	CervixFirmness *string  `json:"cervix_firmness"`
}

type medBrief struct {
	Name string `json:"name"`
	Dose string `json:"dose"`
	Kind string `json:"kind"`
}

// HandleDays returns the merged daily observation view for a date range.
func (d *Deps) HandleDays(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	from, to, ok := dateRange(w, r, -30, 0)
	if !ok {
		return
	}
	ctx := r.Context()
	days := map[string]*dayView{}
	day := func(date string) *dayView {
		if v, ok := days[date]; ok {
			return v
		}
		v := &dayView{Date: date, Symptoms: []symptomBrief{}, Medications: []medBrief{}}
		days[date] = v
		return v
	}

	bleedings, err := d.Q.ListBleedingObservationsByRange(ctx, dbgen.ListBleedingObservationsByRangeParams{
		AccountID: accountID, ObservedDate: from, ObservedDate_2: to,
	})
	if err != nil {
		d.Log.Error("bleeding list failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	for _, row := range bleedings {
		b := rowToBleeding(row)
		day(b.ObservedDate).Bleeding = &bleedingBrief{
			Flow: b.Flow, Intermenstrual: b.Intermenstrual, ProductCount: b.ProductCount,
		}
	}

	entries, err := d.Q.ListDailyEntriesByRange(ctx, dbgen.ListDailyEntriesByRangeParams{
		AccountID: accountID, EntryDate: from, EntryDate_2: to,
	})
	if err != nil {
		d.Log.Error("daily entry list failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	for _, row := range entries {
		e := rowToDailyEntry(row)
		day(e.EntryDate).Entry = &entryBrief{
			PainLevel: e.PainLevel, MoodLevel: e.MoodLevel, EnergyLevel: e.EnergyLevel, Notes: e.Notes,
		}
	}

	logs, err := d.Q.ListSymptomLogsByRange(ctx, dbgen.ListSymptomLogsByRangeParams{
		AccountID: accountID, LogDate: from, LogDate_2: to,
	})
	if err != nil {
		d.Log.Error("symptom log list failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	for _, row := range logs {
		s := rowToSymptomLog(row)
		day(s.LogDate).Symptoms = append(day(s.LogDate).Symptoms, symptomBrief{
			Key: s.SymptomKey, Severity: s.Severity, Notes: s.Notes,
		})
	}

	biomarkers, err := d.Q.ListBiomarkerObservationsByRange(ctx, dbgen.ListBiomarkerObservationsByRangeParams{
		AccountID: accountID, ObservedDate: from, ObservedDate_2: to,
	})
	if err != nil {
		d.Log.Error("biomarker list failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	for _, row := range biomarkers {
		b := rowToBiomarker(row)
		day(b.ObservedDate).Biomarkers = &biomarkerBrief{
			BBTCelsius: b.BBTCelsius, CervicalFluid: b.CervicalFluid,
			CervixPosition: b.CervixPosition, CervixFirmness: b.CervixFirmness,
		}
	}

	meds, err := d.Q.ListMedicationLogsByRange(ctx, dbgen.ListMedicationLogsByRangeParams{
		AccountID: accountID, LogDate: from, LogDate_2: to,
	})
	if err != nil {
		d.Log.Error("medication list failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	for _, row := range meds {
		m := rowToMedication(row)
		day(m.LogDate).Medications = append(day(m.LogDate).Medications, medBrief{
			Name: m.Name, Dose: m.Dose, Kind: m.Kind,
		})
	}

	// Cycle day numbers: the most recent cycle start on or before each date.
	starts := d.cycleStartsUpTo(ctx, accountID, to)
	out := make([]dayView, 0, len(days))
	for _, v := range days {
		v.CycleDay = cycleDayFor(starts, v.Date)
		out = append(out, *v)
	}
	sortDays(out)
	writeJSON(w, http.StatusOK, map[string]any{"days": out})
}

// cycleStartsUpTo returns live cycle starts up to `to`, including starts
// before the requested window so cycle_day can be computed for its first
// days.
func (d *Deps) cycleStartsUpTo(ctx context.Context, accountID, to string) []string {
	rows, err := d.Q.ListCycleStarts(ctx, accountID)
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range rows {
		if s <= to {
			out = append(out, s)
		}
	}
	return out
}

func cycleDayFor(starts []string, date string) *int {
	latest := ""
	for _, s := range starts {
		if s <= date && s > latest {
			latest = s
		}
	}
	if latest == "" {
		return nil
	}
	start, err1 := domain.ParseDate(latest)
	d, err2 := domain.ParseDate(date)
	if err1 != nil || err2 != nil {
		return nil
	}
	cd := int(d.Sub(start).Hours()/24) + 1
	return &cd
}

func sortDays(days []dayView) {
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
}
