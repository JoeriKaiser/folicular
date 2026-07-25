// Package cyclecalc computes cycle estimates from an account's own recorded
// cycle starts. Outputs are ranges with explicit uncertainty, never point
// predictions and never medical conclusions.
//
// Research basis (docs/research/SOURCES.md):
//   - S04 (Bull 2019): no population default; 28-day cycles are a minority,
//     so with zero plausible intervals we return "insufficient", never a
//     guess. A single interval produces a deliberately wide, low-confidence
//     window rather than nothing.
//   - S06 (Chiazze 1968): generous plausibility filter (15-90 days) for
//     intervals; out-of-filter intervals are excluded from estimates but
//     remain valid stored data.
//   - S07 (Mihm 2011): the luteal phase is comparatively stable (~12-14
//     days), so ovulation is anchored backward from the estimated next
//     menstruation (luteal constant 13), not forward at "day 14".
//   - S08 (Fehring 2006): ovulation and fertile-window timing vary widely;
//     calendar-derived ovulation and fertile windows are capped at "low"
//     confidence regardless of history depth.
//   - S09 (Creinin 2004): confidence degrades with observed variability.
package cyclecalc

import (
	"sort"
	"time"
)

const (
	// Method identifies the v1 algorithm in API responses.
	Method = "cycle_length_median_v1"

	minPlausibleIntervalDays = 15 // S06
	maxPlausibleIntervalDays = 90 // S06
	minIntervals             = 1  // at least 2 recorded starts
	singleIntervalRadiusDays = 5  // forced wide radius for a single observation
	recentWindow             = 6  // intervals considered
	minRangeRadiusDays       = 2
	lutealConstantDays       = 13 // S07: ~12-14 days
	spermSurvivalDays        = 5  // fertile window before ovulation
	eggSurvivalDays          = 1  // fertile window after ovulation

	ConfidenceInsufficient = "insufficient"
	ConfidenceLow          = "low"
	ConfidenceModerate     = "moderate"
)

// Disclaimer is the French server-authored copy attached to every estimate
// response. It must never imply certainty or medical advice.
const Disclaimer = "Estimations fondees sur vos cycles enregistres. Ce ne sont pas des previsions certaines ni un avis medical."

type Window struct {
	WindowStart string `json:"window_start"`
	CentralDate string `json:"central_date,omitempty"`
	WindowEnd   string `json:"window_end"`
	Confidence  string `json:"confidence"`
}

type Basis struct {
	CycleCount       int `json:"cycle_count"`
	MedianLengthDays int `json:"median_length_days,omitempty"`
	VariabilityDays  int `json:"variability_days,omitempty"`
}

type Prediction struct {
	GeneratedAt           string  `json:"generated_at"`
	Method                string  `json:"method"`
	Basis                 Basis   `json:"basis"`
	NextMenstruation      *Window `json:"next_menstruation"`
	OvulationEstimate     *Window `json:"ovulation_estimate"`
	FertileWindowEstimate *Window `json:"fertile_window_estimate"`
	Disclaimer            string  `json:"disclaimer"`
}

// Estimate computes ranges from recorded cycle start dates. now is injected
// for deterministic tests.
func Estimate(startDates []time.Time, now time.Time) Prediction {
	p := Prediction{
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Method:      Method,
		Basis:       Basis{CycleCount: len(uniqueSorted(startDates))},
		Disclaimer:  Disclaimer,
	}

	starts := uniqueSorted(startDates)
	intervals := plausibleIntervals(starts)
	if len(intervals) < minIntervals {
		return p // everything nil, basis reports the shortage (S04)
	}
	p.Basis.CycleCount = len(intervals)

	recent := intervals
	if len(recent) > recentWindow {
		recent = recent[len(recent)-recentWindow:]
	}
	median := medianInt(recent)
	variability := recent[len(recent)-1] - recent[0] // recent is sorted ascending
	p.Basis.MedianLengthDays = median
	p.Basis.VariabilityDays = variability

	// A single interval carries no variability signal, so the normal
	// radius formula would understate uncertainty. Force a wide radius
	// to keep the window honest (S04, S09).
	singleInterval := len(recent) == 1
	radius := variability / 2
	if variability%2 != 0 {
		radius++
	}
	if singleInterval {
		radius = singleIntervalRadiusDays
	} else if radius < minRangeRadiusDays {
		radius = minRangeRadiusDays
	}

	lastStart := starts[len(starts)-1]
	central := lastStart.AddDate(0, 0, median)

	// Next menstruation: confidence reflects history depth and variability
	// (S09). Never "certain": moderate is the ceiling. A single interval
	// is always low confidence regardless of the observed length.
	nextConfidence := ConfidenceLow
	if !singleInterval && len(recent) >= recentWindow && variability <= 7 {
		nextConfidence = ConfidenceModerate
	}
	p.NextMenstruation = &Window{
		WindowStart: central.AddDate(0, 0, -radius).Format(time.DateOnly),
		CentralDate: central.Format(time.DateOnly),
		WindowEnd:   central.AddDate(0, 0, radius).Format(time.DateOnly),
		Confidence:  nextConfidence,
	}

	// Ovulation anchored backward from next menstruation (S07), widened by
	// observed variability (S08), and capped at low confidence: calendar
	// timing alone is a weak signal.
	ovulationRadius := 2 + radius
	ovulationCentral := central.AddDate(0, 0, -lutealConstantDays)
	p.OvulationEstimate = &Window{
		WindowStart: ovulationCentral.AddDate(0, 0, -ovulationRadius).Format(time.DateOnly),
		CentralDate: ovulationCentral.Format(time.DateOnly),
		WindowEnd:   ovulationCentral.AddDate(0, 0, ovulationRadius).Format(time.DateOnly),
		Confidence:  ConfidenceLow,
	}

	p.FertileWindowEstimate = &Window{
		WindowStart: ovulationCentral.AddDate(0, 0, -spermSurvivalDays).Format(time.DateOnly),
		WindowEnd:   ovulationCentral.AddDate(0, 0, eggSurvivalDays).Format(time.DateOnly),
		Confidence:  ConfidenceLow,
	}

	return p
}

func uniqueSorted(dates []time.Time) []time.Time {
	seen := make(map[string]time.Time, len(dates))
	for _, d := range dates {
		seen[d.Format(time.DateOnly)] = d
	}
	out := make([]time.Time, 0, len(seen))
	for _, d := range seen {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

func plausibleIntervals(starts []time.Time) []int {
	var out []int
	for i := 1; i < len(starts); i++ {
		days := int(starts[i].Sub(starts[i-1]).Hours() / 24)
		if days >= minPlausibleIntervalDays && days <= maxPlausibleIntervalDays {
			out = append(out, days)
		}
	}
	sort.Ints(out)
	return out
}

func medianInt(sorted []int) int {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2] + 1) / 2 // round half up
}
