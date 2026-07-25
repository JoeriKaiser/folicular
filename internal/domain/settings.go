package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Life stages aligned with STRAW+10 (S11). User-selected, never inferred.
var LifeStages = []string{
	"unknown",
	"reproductive_early",
	"reproductive_peak",
	"reproductive_late",
	"menopause_transition_early",
	"menopause_transition_late",
	"postmenopause_early",
	"postmenopause_late",
}

// Tracking focuses: user-selected charting contexts, never diagnoses
// (S20-S25).
var TrackingFocuses = []string{"pms", "pmdd", "endometriosis", "pcos", "custom"}

// Settings is the server-authoritative account settings payload.
type Settings struct {
	Locale        string   `json:"locale"`
	TimeZone      string   `json:"time_zone"`
	LifeStage     string   `json:"life_stage"`
	TrackingFocus []string `json:"tracking_focus"`
	UpdatedAt     string   `json:"updated_at"`
}

// ValidateSettings checks a full settings state.
func ValidateSettings(s Settings) error {
	var errs []string
	if len(s.Locale) == 0 || len(s.Locale) > 35 {
		errs = append(errs, "locale: longueur entre 1 et 35 requise")
	}
	if _, err := time.LoadLocation(s.TimeZone); err != nil {
		errs = append(errs, "time_zone: fuseau IANA invalide")
	}
	if !oneOf(s.LifeStage, LifeStages...) {
		errs = append(errs, "life_stage: valeur invalide")
	}
	if len(s.TrackingFocus) > 10 {
		errs = append(errs, "tracking_focus: au plus 10 valeurs")
	}
	for _, f := range s.TrackingFocus {
		if !oneOf(f, TrackingFocuses...) {
			errs = append(errs, fmt.Sprintf("tracking_focus: valeur invalide %q", f))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("settings: %s", strings.Join(errs, "; "))
	}
	return nil
}

// EncodeTrackingFocus serializes focuses for storage.
func EncodeTrackingFocus(focuses []string) string {
	if focuses == nil {
		focuses = []string{}
	}
	b, _ := json.Marshal(focuses)
	return string(b)
}

// DecodeTrackingFocus parses stored focuses, tolerating malformed storage.
func DecodeTrackingFocus(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	return out
}
