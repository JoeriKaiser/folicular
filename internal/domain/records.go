package domain

import (
	"regexp"
	"time"
)

// Cycle: start_date is cycle day 1, the first day of full menstrual flow
// (S01). length_days bounds are permissive plausibility, not normative
// ranges (S03, S06).
type Cycle struct {
	Envelope
	StartDate    string  `json:"start_date"`
	EndDate      *string `json:"end_date"`
	LengthDays   *int    `json:"length_days"`
	BleedingDays *int    `json:"bleeding_days"`
	Certainty    string  `json:"certainty"`
	Source       string  `json:"source"`
	Notes        string  `json:"notes"`
}

func (c Cycle) Validate() error {
	var errs []string
	if err := c.Envelope.validate(); err != nil {
		errs = append(errs, err.Error())
	}
	if !isDate(c.StartDate) {
		errs = append(errs, "start_date: date ISO invalide")
	}
	if c.EndDate != nil {
		if !isDate(*c.EndDate) {
			errs = append(errs, "end_date: date ISO invalide")
		} else if isDate(c.StartDate) && *c.EndDate < c.StartDate {
			errs = append(errs, "end_date: antérieur à start_date")
		}
	}
	if c.LengthDays != nil && (*c.LengthDays < 10 || *c.LengthDays > 200) {
		errs = append(errs, "length_days: doit être compris entre 10 et 200")
	}
	if c.BleedingDays != nil && (*c.BleedingDays < 0 || *c.BleedingDays > 45) {
		errs = append(errs, "bleeding_days: doit être compris entre 0 et 45")
	}
	if !oneOf(c.Certainty, "recorded", "uncertain", "estimated") {
		errs = append(errs, "certainty: valeur invalide")
	}
	if !oneOf(c.Source, "manual", "import", "estimated") {
		errs = append(errs, "source: valeur invalide")
	}
	errs = checkNotes(c.Notes, errs)
	return joinErrs("cycle", errs)
}

// BleedingObservation: FIGO-aligned self-rated flow (S01, S02). Spotting is
// flagged as intermenstrual bleeding; no cause taxonomy exists.
type BleedingObservation struct {
	Envelope
	ObservedDate  string `json:"observed_date"`
	Flow          string `json:"flow"`
	Intermenstrual bool  `json:"intermenstrual"`
	ProductCount  *int   `json:"product_count"`
	Notes         string `json:"notes"`
}

func (b BleedingObservation) Validate() error {
	var errs []string
	if err := b.Envelope.validate(); err != nil {
		errs = append(errs, err.Error())
	}
	if !isDate(b.ObservedDate) {
		errs = append(errs, "observed_date: date ISO invalide")
	}
	if !oneOf(b.Flow, "none", "spotting", "light", "medium", "heavy") {
		errs = append(errs, "flow: valeur invalide")
	}
	if b.ProductCount != nil && (*b.ProductCount < 0 || *b.ProductCount > 60) {
		errs = append(errs, "product_count: doit être compris entre 0 et 60")
	}
	errs = checkNotes(b.Notes, errs)
	return joinErrs("bleeding_observation", errs)
}

// DailyEntry: prospective daily charting with 1-5 severity scales (S25).
// Nullable levels mean "not recorded", never zero.
type DailyEntry struct {
	Envelope
	EntryDate   string `json:"entry_date"`
	PainLevel   *int   `json:"pain_level"`
	MoodLevel   *int   `json:"mood_level"`
	EnergyLevel *int   `json:"energy_level"`
	Notes       string `json:"notes"`
}

func (d DailyEntry) Validate() error {
	var errs []string
	if err := d.Envelope.validate(); err != nil {
		errs = append(errs, err.Error())
	}
	if !isDate(d.EntryDate) {
		errs = append(errs, "entry_date: date ISO invalide")
	}
	if d.PainLevel != nil && (*d.PainLevel < 1 || *d.PainLevel > 5) {
		errs = append(errs, "pain_level: doit être compris entre 1 et 5")
	}
	if d.MoodLevel != nil && (*d.MoodLevel < 1 || *d.MoodLevel > 5) {
		errs = append(errs, "mood_level: doit être compris entre 1 et 5")
	}
	if d.EnergyLevel != nil && (*d.EnergyLevel < 1 || *d.EnergyLevel > 5) {
		errs = append(errs, "energy_level: doit être compris entre 1 et 5")
	}
	errs = checkNotes(d.Notes, errs)
	return joinErrs("daily_entry", errs)
}

var keyPattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// SymptomDefinition: per-account catalog entry (built-in seed or custom).
type SymptomDefinition struct {
	Envelope
	Key      string `json:"key"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Builtin  bool   `json:"builtin"`
	Active   bool   `json:"active"`
}

func (s SymptomDefinition) Validate() error {
	var errs []string
	if err := s.Envelope.validate(); err != nil {
		errs = append(errs, err.Error())
	}
	if !keyPattern.MatchString(s.Key) {
		errs = append(errs, "key: doit matcher [a-z0-9_]{1,64}")
	}
	if len(s.Label) == 0 || len(s.Label) > 120 {
		errs = append(errs, "label: longueur entre 1 et 120 requise")
	}
	if !oneOf(s.Category, "mood", "physical", "energy", "pain", "cervical_fluid", "other") {
		errs = append(errs, "category: valeur invalide")
	}
	return joinErrs("symptom_definition", errs)
}

// SymptomLog: point observation; symptom_key references a definition loosely
// so logs survive definition edits.
type SymptomLog struct {
	Envelope
	LogDate    string `json:"log_date"`
	LoggedAt   string `json:"logged_at"`
	SymptomKey string `json:"symptom_key"`
	Severity   int    `json:"severity"`
	Notes      string `json:"notes"`
}

func (s SymptomLog) Validate() error {
	var errs []string
	if err := s.Envelope.validate(); err != nil {
		errs = append(errs, err.Error())
	}
	if !isDate(s.LogDate) {
		errs = append(errs, "log_date: date ISO invalide")
	}
	if !isInstant(s.LoggedAt) {
		errs = append(errs, "logged_at: instant RFC 3339 invalide")
	}
	if !keyPattern.MatchString(s.SymptomKey) {
		errs = append(errs, "symptom_key: doit matcher [a-z0-9_]{1,64}")
	}
	if s.Severity < 1 || s.Severity > 5 {
		errs = append(errs, "severity: doit être compris entre 1 et 5")
	}
	errs = checkNotes(s.Notes, errs)
	return joinErrs("symptom_log", errs)
}

// BiomarkerObservation: self-observed, probabilistic signals stored as
// recorded (S07, S08). The v1 estimate engine does not consume them.
type BiomarkerObservation struct {
	Envelope
	ObservedDate   string  `json:"observed_date"`
	BBTCelsius     *float64 `json:"bbt_celsius"`
	BBTTime        *string `json:"bbt_time"`
	BBTQuality     string  `json:"bbt_quality"`
	CervicalFluid  *string `json:"cervical_fluid"`
	CervixPosition *string `json:"cervix_position"`
	CervixFirmness *string `json:"cervix_firmness"`
	Notes          string  `json:"notes"`
}

func (b BiomarkerObservation) Validate() error {
	var errs []string
	if err := b.Envelope.validate(); err != nil {
		errs = append(errs, err.Error())
	}
	if !isDate(b.ObservedDate) {
		errs = append(errs, "observed_date: date ISO invalide")
	}
	if b.BBTCelsius != nil && (*b.BBTCelsius < 34.0 || *b.BBTCelsius > 43.0) {
		errs = append(errs, "bbt_celsius: doit être compris entre 34.0 et 43.0")
	}
	if b.BBTTime != nil && !isHHMM(*b.BBTTime) {
		errs = append(errs, "bbt_time: format HH:MM invalide")
	}
	if !oneOf(b.BBTQuality, "normal", "disturbed") {
		errs = append(errs, "bbt_quality: valeur invalide")
	}
	if b.CervicalFluid != nil && !oneOf(*b.CervicalFluid, "none", "sticky", "creamy", "watery", "egg_white", "unresolved") {
		errs = append(errs, "cervical_fluid: valeur invalide")
	}
	if b.CervixPosition != nil && !oneOf(*b.CervixPosition, "low", "medium", "high", "unknown") {
		errs = append(errs, "cervix_position: valeur invalide")
	}
	if b.CervixFirmness != nil && !oneOf(*b.CervixFirmness, "firm", "soft", "unknown") {
		errs = append(errs, "cervix_firmness: valeur invalide")
	}
	errs = checkNotes(b.Notes, errs)
	return joinErrs("biomarker_observation", errs)
}

// MedicationLog: hormonal contraception is tracked as context because it
// changes bleeding patterns; it is never used to infer anything.
type MedicationLog struct {
	Envelope
	LogDate string  `json:"log_date"`
	TakenAt *string `json:"taken_at"`
	Name    string  `json:"name"`
	Dose    string  `json:"dose"`
	Kind    string  `json:"kind"`
	Notes   string  `json:"notes"`
}

func (m MedicationLog) Validate() error {
	var errs []string
	if err := m.Envelope.validate(); err != nil {
		errs = append(errs, err.Error())
	}
	if !isDate(m.LogDate) {
		errs = append(errs, "log_date: date ISO invalide")
	}
	if m.TakenAt != nil && !isInstant(*m.TakenAt) && !isHHMM(*m.TakenAt) {
		errs = append(errs, "taken_at: format invalide (RFC 3339 ou HH:MM attendu)")
	}
	if len(m.Name) == 0 || len(m.Name) > 200 {
		errs = append(errs, "name: longueur entre 1 et 200 requise")
	}
	if len(m.Dose) > 100 {
		errs = append(errs, "dose: dépasse la longueur maximale")
	}
	if !oneOf(m.Kind, "contraceptive_hormonal", "emergency_contraception", "pain_relief", "supplement", "other") {
		errs = append(errs, "kind: valeur invalide")
	}
	errs = checkNotes(m.Notes, errs)
	return joinErrs("medication_log", errs)
}

// ParseDate parses an ISO calendar date.
func ParseDate(s string) (time.Time, error) {
	return time.Parse(time.DateOnly, s)
}
