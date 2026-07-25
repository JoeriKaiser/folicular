// Package domain defines the canonical synchronized record types and their
// validation rules. This package is the contract: handlers parse, domain
// validates, sqlc persists. Every range and enum here traces to
// docs/research/SOURCES.md (see docs/data-model.md).
package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Entity types replicated through the sync change log.
const (
	TypeCycle                 = "cycle"
	TypeBleedingObservation   = "bleeding_observation"
	TypeDailyEntry            = "daily_entry"
	TypeSymptomDefinition     = "symptom_definition"
	TypeSymptomLog            = "symptom_log"
	TypeBiomarkerObservation  = "biomarker_observation"
	TypeMedicationLog         = "medication_log"
)

// EntityTypes lists all synchronized entity types.
var EntityTypes = []string{
	TypeCycle, TypeBleedingObservation, TypeDailyEntry, TypeSymptomDefinition,
	TypeSymptomLog, TypeBiomarkerObservation, TypeMedicationLog,
}

// Envelope is shared by every synchronized record. See docs/data-model.md.
type Envelope struct {
	ID        string  `json:"id"`
	ClientRev string  `json:"client_rev"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	DeletedAt *string `json:"deleted_at"`
}

func (e Envelope) validate() error {
	var errs []string
	if _, err := uuid.Parse(e.ID); err != nil {
		errs = append(errs, "id: UUID invalide")
	}
	if _, err := uuid.Parse(e.ClientRev); err != nil {
		errs = append(errs, "client_rev: UUID invalide")
	}
	if !isInstant(e.CreatedAt) {
		errs = append(errs, "created_at: instant RFC 3339 invalide")
	}
	if !isInstant(e.UpdatedAt) {
		errs = append(errs, "updated_at: instant RFC 3339 invalide")
	} else if t, err := time.Parse(time.RFC3339, e.UpdatedAt); err == nil {
		if t.After(time.Now().Add(5 * time.Minute)) {
			errs = append(errs, "updated_at: horodatage trop éloigné dans le futur (horloge décalée)")
		}
	}
	if e.DeletedAt != nil && !isInstant(*e.DeletedAt) {
		errs = append(errs, "deleted_at: instant RFC 3339 invalide")
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// IsDeleted reports whether the envelope carries a tombstone.
func (e Envelope) IsDeleted() bool { return e.DeletedAt != nil }

func isInstant(s string) bool {
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

func isDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

func isHHMM(s string) bool {
	_, err := time.Parse("15:04", s)
	return err == nil
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func joinErrs(prefix string, errs []string) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", prefix, strings.Join(errs, "; "))
}

const maxNotesLen = 5000

func checkNotes(notes string, errs []string) []string {
	if len(notes) > maxNotesLen {
		errs = append(errs, "notes: dépasse la longueur maximale")
	}
	return errs
}
