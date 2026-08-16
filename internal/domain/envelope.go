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

// IsInstant reports whether s is a valid RFC 3339 instant.
func IsInstant(s string) bool { return isInstant(s) }

// IsUUID reports whether s is a valid UUID.
func IsUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// IsEntityType reports whether s is a synchronized entity type.
//
// With end-to-end encryption the server validates the routing envelope and
// nothing else: the payload is ciphertext it cannot read, so content rules
// (ranges, enums, cross-field checks) belong to the client, which is the only
// party able to apply them.
func IsEntityType(s string) bool {
	for _, t := range EntityTypes {
		if s == t {
			return true
		}
	}
	return false
}
