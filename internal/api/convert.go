package api

import (
	"database/sql"

	"folicular/internal/db/dbgen"
	"folicular/internal/domain"
)

// Nullable helpers between domain pointers and sqlc null types.

func ns(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func sp(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func ni(p *int) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}

func ip(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

func nf(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}

func fp(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Cycles ----------------------------------------------------------------

func cycleParams(accountID string, c domain.Cycle) dbgen.UpsertCycleParams {
	return dbgen.UpsertCycleParams{
		ID:           c.ID,
		AccountID:    accountID,
		StartDate:    c.StartDate,
		EndDate:      ns(c.EndDate),
		LengthDays:   ni(c.LengthDays),
		BleedingDays: ni(c.BleedingDays),
		Certainty:    c.Certainty,
		Source:       c.Source,
		Notes:        c.Notes,
		ClientRev:    c.ClientRev,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
		DeletedAt:    ns(c.DeletedAt),
	}
}

func rowToCycle(r dbgen.Cycle) domain.Cycle {
	return domain.Cycle{
		Envelope: domain.Envelope{
			ID: r.ID, ClientRev: r.ClientRev, CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt, DeletedAt: sp(r.DeletedAt),
		},
		StartDate:    r.StartDate,
		EndDate:      sp(r.EndDate),
		LengthDays:   ip(r.LengthDays),
		BleedingDays: ip(r.BleedingDays),
		Certainty:    r.Certainty,
		Source:       r.Source,
		Notes:        r.Notes,
	}
}

// Bleeding observations ----------------------------------------------------

func bleedingParams(accountID string, b domain.BleedingObservation) dbgen.UpsertBleedingObservationParams {
	return dbgen.UpsertBleedingObservationParams{
		ID:             b.ID,
		AccountID:      accountID,
		ObservedDate:   b.ObservedDate,
		Flow:           b.Flow,
		Intermenstrual: boolInt(b.Intermenstrual),
		ProductCount:   ni(b.ProductCount),
		Notes:          b.Notes,
		ClientRev:      b.ClientRev,
		CreatedAt:      b.CreatedAt,
		UpdatedAt:      b.UpdatedAt,
		DeletedAt:      ns(b.DeletedAt),
	}
}

func rowToBleeding(r dbgen.BleedingObservation) domain.BleedingObservation {
	return domain.BleedingObservation{
		Envelope: domain.Envelope{
			ID: r.ID, ClientRev: r.ClientRev, CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt, DeletedAt: sp(r.DeletedAt),
		},
		ObservedDate:   r.ObservedDate,
		Flow:           r.Flow,
		Intermenstrual: r.Intermenstrual == 1,
		ProductCount:   ip(r.ProductCount),
		Notes:          r.Notes,
	}
}

// Daily entries ---------------------------------------------------------

func dailyEntryParams(accountID string, e domain.DailyEntry) dbgen.UpsertDailyEntryParams {
	return dbgen.UpsertDailyEntryParams{
		ID:          e.ID,
		AccountID:   accountID,
		EntryDate:   e.EntryDate,
		PainLevel:   ni(e.PainLevel),
		MoodLevel:   ni(e.MoodLevel),
		EnergyLevel: ni(e.EnergyLevel),
		Notes:       e.Notes,
		ClientRev:   e.ClientRev,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
		DeletedAt:   ns(e.DeletedAt),
	}
}

func rowToDailyEntry(r dbgen.DailyEntry) domain.DailyEntry {
	return domain.DailyEntry{
		Envelope: domain.Envelope{
			ID: r.ID, ClientRev: r.ClientRev, CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt, DeletedAt: sp(r.DeletedAt),
		},
		EntryDate:   r.EntryDate,
		PainLevel:   ip(r.PainLevel),
		MoodLevel:   ip(r.MoodLevel),
		EnergyLevel: ip(r.EnergyLevel),
		Notes:       r.Notes,
	}
}

// Symptom definitions ------------------------------------------------------

func symptomDefParams(accountID string, s domain.SymptomDefinition) dbgen.UpsertSymptomDefinitionParams {
	return dbgen.UpsertSymptomDefinitionParams{
		ID:         s.ID,
		AccountID:  accountID,
		Key:        s.Key,
		Label:      s.Label,
		Category:   s.Category,
		Builtin:    boolInt(s.Builtin),
		Active:     boolInt(s.Active),
		ClientRev:  s.ClientRev,
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
		DeletedAt:  ns(s.DeletedAt),
	}
}

func rowToSymptomDef(r dbgen.SymptomDefinition) domain.SymptomDefinition {
	return domain.SymptomDefinition{
		Envelope: domain.Envelope{
			ID: r.ID, ClientRev: r.ClientRev, CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt, DeletedAt: sp(r.DeletedAt),
		},
		Key:      r.Key,
		Label:    r.Label,
		Category: r.Category,
		Builtin:  r.Builtin == 1,
		Active:   r.Active == 1,
	}
}

// Symptom logs ------------------------------------------------------------

func symptomLogParams(accountID string, s domain.SymptomLog) dbgen.UpsertSymptomLogParams {
	return dbgen.UpsertSymptomLogParams{
		ID:         s.ID,
		AccountID:  accountID,
		LogDate:    s.LogDate,
		LoggedAt:   s.LoggedAt,
		SymptomKey: s.SymptomKey,
		Severity:   int64(s.Severity),
		Notes:      s.Notes,
		ClientRev:  s.ClientRev,
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
		DeletedAt:  ns(s.DeletedAt),
	}
}

func rowToSymptomLog(r dbgen.SymptomLog) domain.SymptomLog {
	return domain.SymptomLog{
		Envelope: domain.Envelope{
			ID: r.ID, ClientRev: r.ClientRev, CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt, DeletedAt: sp(r.DeletedAt),
		},
		LogDate:    r.LogDate,
		LoggedAt:   r.LoggedAt,
		SymptomKey: r.SymptomKey,
		Severity:   int(r.Severity),
		Notes:      r.Notes,
	}
}

// Biomarker observations ----------------------------------------------------

func biomarkerParams(accountID string, b domain.BiomarkerObservation) dbgen.UpsertBiomarkerObservationParams {
	return dbgen.UpsertBiomarkerObservationParams{
		ID:             b.ID,
		AccountID:      accountID,
		ObservedDate:   b.ObservedDate,
		BbtCelsius:     nf(b.BBTCelsius),
		BbtTime:        ns(b.BBTTime),
		BbtQuality:     b.BBTQuality,
		CervicalFluid:  ns(b.CervicalFluid),
		CervixPosition: ns(b.CervixPosition),
		CervixFirmness: ns(b.CervixFirmness),
		Notes:          b.Notes,
		ClientRev:      b.ClientRev,
		CreatedAt:      b.CreatedAt,
		UpdatedAt:      b.UpdatedAt,
		DeletedAt:      ns(b.DeletedAt),
	}
}

func rowToBiomarker(r dbgen.BiomarkerObservation) domain.BiomarkerObservation {
	return domain.BiomarkerObservation{
		Envelope: domain.Envelope{
			ID: r.ID, ClientRev: r.ClientRev, CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt, DeletedAt: sp(r.DeletedAt),
		},
		ObservedDate:   r.ObservedDate,
		BBTCelsius:     fp(r.BbtCelsius),
		BBTTime:        sp(r.BbtTime),
		BBTQuality:     r.BbtQuality,
		CervicalFluid:  sp(r.CervicalFluid),
		CervixPosition: sp(r.CervixPosition),
		CervixFirmness: sp(r.CervixFirmness),
		Notes:          r.Notes,
	}
}

// Medication logs ---------------------------------------------------------

func medicationParams(accountID string, m domain.MedicationLog) dbgen.UpsertMedicationLogParams {
	return dbgen.UpsertMedicationLogParams{
		ID:        m.ID,
		AccountID: accountID,
		LogDate:   m.LogDate,
		TakenAt:   ns(m.TakenAt),
		Name:      m.Name,
		Dose:      m.Dose,
		Kind:      m.Kind,
		Notes:     m.Notes,
		ClientRev: m.ClientRev,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
		DeletedAt: ns(m.DeletedAt),
	}
}

func rowToMedication(r dbgen.MedicationLog) domain.MedicationLog {
	return domain.MedicationLog{
		Envelope: domain.Envelope{
			ID: r.ID, ClientRev: r.ClientRev, CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt, DeletedAt: sp(r.DeletedAt),
		},
		LogDate: r.LogDate,
		TakenAt: sp(r.TakenAt),
		Name:    r.Name,
		Dose:    r.Dose,
		Kind:    r.Kind,
		Notes:   r.Notes,
	}
}
