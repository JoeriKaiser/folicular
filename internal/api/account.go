package api

import (
	"net/http"
	"time"

	"folicular/internal/api/problem"
	"folicular/internal/auth"
	"folicular/internal/db/dbgen"
	"folicular/internal/domain"
)

const (
	exportDateMin = "0001-01-01"
	exportDateMax = "9999-12-31"
)

type meResponse struct {
	Account  accountInfo     `json:"account"`
	Device   deviceShort     `json:"device"`
	Settings settingsPayload `json:"settings"`
}

type accountInfo struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type deviceShort struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type settingsPayload struct {
	Locale        string   `json:"locale"`
	TimeZone      string   `json:"time_zone"`
	LifeStage     string   `json:"life_stage"`
	TrackingFocus []string `json:"tracking_focus"`
	UpdatedAt     string   `json:"updated_at"`
}

// HandleMe returns the account, current device, and server-authoritative
// settings.
func (d *Deps) HandleMe(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	deviceID, _ := auth.DeviceID(r.Context())
	deviceName, _ := auth.DeviceName(r.Context())

	account, err := d.Q.GetAccountByID(r.Context(), accountID)
	if err != nil {
		d.Log.Error("account lookup failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	settings, err := d.Q.GetSettings(r.Context(), accountID)
	if err != nil {
		d.Log.Error("settings lookup failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	writeJSON(w, http.StatusOK, meResponse{
		Account:  accountInfo{ID: account.ID, Status: account.Status, CreatedAt: account.CreatedAt},
		Device:   deviceShort{ID: deviceID, Name: deviceName},
		Settings: settingsToPayload(settings),
	})
}

func settingsToPayload(s dbgen.AccountSetting) settingsPayload {
	return settingsPayload{
		Locale:        s.Locale,
		TimeZone:      s.TimeZone,
		LifeStage:     s.LifeStage,
		TrackingFocus: domain.DecodeTrackingFocus(s.TrackingFocus),
		UpdatedAt:     s.UpdatedAt,
	}
}

type patchSettingsRequest struct {
	Locale        *string   `json:"locale"`
	TimeZone      *string   `json:"time_zone"`
	LifeStage     *string   `json:"life_stage"`
	TrackingFocus *[]string `json:"tracking_focus"`
}

// HandlePatchMe partially updates server-authoritative settings. The merged
// state is validated as a whole before being written.
func (d *Deps) HandlePatchMe(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())

	var req patchSettingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	current, err := d.Q.GetSettings(r.Context(), accountID)
	if err != nil {
		d.Log.Error("settings lookup failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	merged := domain.Settings{
		Locale:        current.Locale,
		TimeZone:      current.TimeZone,
		LifeStage:     current.LifeStage,
		TrackingFocus: domain.DecodeTrackingFocus(current.TrackingFocus),
	}
	if req.Locale != nil {
		merged.Locale = *req.Locale
	}
	if req.TimeZone != nil {
		merged.TimeZone = *req.TimeZone
	}
	if req.LifeStage != nil {
		merged.LifeStage = *req.LifeStage
	}
	if req.TrackingFocus != nil {
		merged.TrackingFocus = *req.TrackingFocus
	}
	if err := domain.ValidateSettings(merged); err != nil {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée", err.Error()))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.Q.UpdateSettings(r.Context(), dbgen.UpdateSettingsParams{
		Locale:        merged.Locale,
		TimeZone:      merged.TimeZone,
		LifeStage:     merged.LifeStage,
		TrackingFocus: domain.EncodeTrackingFocus(merged.TrackingFocus),
		UpdatedAt:     now,
		AccountID:     accountID,
	}); err != nil {
		d.Log.Error("settings update failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	// Return the full /v1/me body for client convenience.
	d.HandleMe(w, r)
}

// HandleDeleteMe permanently deletes the account and all associated data.
// The schema uses ON DELETE CASCADE on every table referencing accounts,
// so a single DELETE FROM accounts removes everything (GDPR Article 17).
func (d *Deps) HandleDeleteMe(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	if err := d.Q.DeleteAccount(r.Context(), accountID); err != nil {
		d.Log.Error("account deletion failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	d.Log.Info("account deleted", "account_id", accountID)
	w.WriteHeader(http.StatusNoContent)
}

// HandleExport returns all live records for the account as a single JSON
// document (GDPR Article 20, data portability). Uses the existing range
// queries with a maximal window to collect every non-deleted record.
func (d *Deps) HandleExport(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	ctx := r.Context()
	params := dbgen.ListCyclesByRangeParams{AccountID: accountID, StartDate: exportDateMin, StartDate_2: exportDateMax}

	cycles, err := d.Q.ListCyclesByRange(ctx, params)
	if err != nil {
		d.Log.Error("export: cycles failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	bleedings, err := d.Q.ListBleedingObservationsByRange(ctx, dbgen.ListBleedingObservationsByRangeParams{
		AccountID: accountID, ObservedDate: exportDateMin, ObservedDate_2: exportDateMax,
	})
	if err != nil {
		d.Log.Error("export: bleedings failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	entries, err := d.Q.ListDailyEntriesByRange(ctx, dbgen.ListDailyEntriesByRangeParams{
		AccountID: accountID, EntryDate: exportDateMin, EntryDate_2: exportDateMax,
	})
	if err != nil {
		d.Log.Error("export: daily entries failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	symptomDefs, err := d.Q.ListSymptomDefinitions(ctx, accountID)
	if err != nil {
		d.Log.Error("export: symptom definitions failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	symptomLogs, err := d.Q.ListSymptomLogsByRange(ctx, dbgen.ListSymptomLogsByRangeParams{
		AccountID: accountID, LogDate: exportDateMin, LogDate_2: exportDateMax,
	})
	if err != nil {
		d.Log.Error("export: symptom logs failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	biomarkers, err := d.Q.ListBiomarkerObservationsByRange(ctx, dbgen.ListBiomarkerObservationsByRangeParams{
		AccountID: accountID, ObservedDate: exportDateMin, ObservedDate_2: exportDateMax,
	})
	if err != nil {
		d.Log.Error("export: biomarkers failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	meds, err := d.Q.ListMedicationLogsByRange(ctx, dbgen.ListMedicationLogsByRangeParams{
		AccountID: accountID, LogDate: exportDateMin, LogDate_2: exportDateMax,
	})
	if err != nil {
		d.Log.Error("export: medications failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	outCycles := make([]domain.Cycle, 0, len(cycles))
	for _, row := range cycles {
		outCycles = append(outCycles, rowToCycle(row))
	}
	outBleedings := make([]domain.BleedingObservation, 0, len(bleedings))
	for _, row := range bleedings {
		outBleedings = append(outBleedings, rowToBleeding(row))
	}
	outEntries := make([]domain.DailyEntry, 0, len(entries))
	for _, row := range entries {
		outEntries = append(outEntries, rowToDailyEntry(row))
	}
	outSymptomDefs := make([]domain.SymptomDefinition, 0, len(symptomDefs))
	for _, row := range symptomDefs {
		outSymptomDefs = append(outSymptomDefs, rowToSymptomDef(row))
	}
	outSymptomLogs := make([]domain.SymptomLog, 0, len(symptomLogs))
	for _, row := range symptomLogs {
		outSymptomLogs = append(outSymptomLogs, rowToSymptomLog(row))
	}
	outBiomarkers := make([]domain.BiomarkerObservation, 0, len(biomarkers))
	for _, row := range biomarkers {
		outBiomarkers = append(outBiomarkers, rowToBiomarker(row))
	}
	outMeds := make([]domain.MedicationLog, 0, len(meds))
	for _, row := range meds {
		outMeds = append(outMeds, rowToMedication(row))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"exported_at":            time.Now().UTC().Format(time.RFC3339),
		"cycles":                 outCycles,
		"bleeding_observations":  outBleedings,
		"daily_entries":          outEntries,
		"symptom_definitions":    outSymptomDefs,
		"symptom_logs":           outSymptomLogs,
		"biomarker_observations": outBiomarkers,
		"medication_logs":        outMeds,
	})
}
