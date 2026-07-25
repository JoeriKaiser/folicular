package cyclecalc

import (
	"testing"
	"time"
)

func dates(ss ...string) []time.Time {
	out := make([]time.Time, 0, len(ss))
	for _, s := range ss {
		t, err := time.Parse(time.DateOnly, s)
		if err != nil {
			panic(err)
		}
		out = append(out, t)
	}
	return out
}

var now = time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

func TestEstimateInsufficientHistory(t *testing.T) {
	// A single start yields zero plausible intervals: no estimate (S04).
	p := Estimate(dates("2026-06-01"), now)
	if p.NextMenstruation != nil || p.OvulationEstimate != nil || p.FertileWindowEstimate != nil {
		t.Fatalf("expected nil windows with insufficient history, got %+v", p)
	}
	if p.Basis.CycleCount != 1 {
		t.Fatalf("basis.cycle_count = %d, want 1", p.Basis.CycleCount)
	}
	if p.Method != Method || p.Disclaimer == "" {
		t.Fatalf("method/disclaimer missing: %+v", p)
	}
}

func TestEstimateSingleInterval(t *testing.T) {
	// Two starts produce one interval: a deliberately wide, low-confidence
	// window rather than nothing.
	p := Estimate(dates("2026-06-01", "2026-06-30"), now)
	if p.NextMenstruation == nil {
		t.Fatal("expected a window from a single interval")
	}
	if p.NextMenstruation.Confidence != ConfidenceLow {
		t.Fatalf("confidence = %q, want low", p.NextMenstruation.Confidence)
	}
	if p.Basis.CycleCount != 1 {
		t.Fatalf("basis.cycle_count = %d, want 1", p.Basis.CycleCount)
	}
	if p.Basis.MedianLengthDays != 29 {
		t.Fatalf("median = %d, want 29", p.Basis.MedianLengthDays)
	}
	// Radius must be the forced single-interval radius (5 days each side).
	start, _ := time.Parse(time.DateOnly, p.NextMenstruation.WindowStart)
	end, _ := time.Parse(time.DateOnly, p.NextMenstruation.WindowEnd)
	if days := int(end.Sub(start).Hours() / 24); days != 10 {
		t.Fatalf("window width = %d days, want 10 (radius 5)", days)
	}
	// Central = last start + 29 days.
	if p.NextMenstruation.CentralDate != "2026-07-29" {
		t.Fatalf("central = %s, want 2026-07-29", p.NextMenstruation.CentralDate)
	}
	// Ovulation and fertile window still present, capped at low.
	if p.OvulationEstimate == nil || p.OvulationEstimate.Confidence != ConfidenceLow {
		t.Fatal("ovulation window missing or not low confidence")
	}
	if p.FertileWindowEstimate == nil || p.FertileWindowEstimate.Confidence != ConfidenceLow {
		t.Fatal("fertile window missing or not low confidence")
	}
}

func TestEstimateRegularCycles(t *testing.T) {
	// Seven starts, six 29-day intervals: moderate confidence, tight window.
	p := Estimate(dates(
		"2026-01-01", "2026-01-30", "2026-02-28", "2026-03-29",
		"2026-04-27", "2026-05-26", "2026-06-24",
	), now)
	if p.NextMenstruation == nil {
		t.Fatal("expected next menstruation window")
	}
	if got := p.Basis.MedianLengthDays; got != 29 {
		t.Fatalf("median = %d, want 29", got)
	}
	if p.NextMenstruation.Confidence != ConfidenceModerate {
		t.Fatalf("confidence = %q, want moderate", p.NextMenstruation.Confidence)
	}
	// Central = last start + 29 days.
	if p.NextMenstruation.CentralDate != "2026-07-23" {
		t.Fatalf("central = %s, want 2026-07-23", p.NextMenstruation.CentralDate)
	}
	// Ovulation anchored backward by the luteal constant (S07), not day 14.
	if p.OvulationEstimate.CentralDate != "2026-07-10" {
		t.Fatalf("ovulation central = %s, want 2026-07-10", p.OvulationEstimate.CentralDate)
	}
	if p.OvulationEstimate.Confidence != ConfidenceLow || p.FertileWindowEstimate.Confidence != ConfidenceLow {
		t.Fatal("calendar-derived ovulation/fertile windows must be capped at low confidence (S08)")
	}
}

func TestEstimateIrregularCyclesWidenAndDegrade(t *testing.T) {
	// Intervals 24, 26, 40, 45, 60: high variability -> low confidence,
	// wide window (S09).
	p := Estimate(dates(
		"2025-11-01", "2025-11-25", "2025-12-21", "2026-01-30",
		"2026-03-06", "2026-05-05",
	), now)
	if p.NextMenstruation == nil {
		t.Fatal("expected a window even for irregular cycles")
	}
	if p.NextMenstruation.Confidence != ConfidenceLow {
		t.Fatalf("confidence = %q, want low", p.NextMenstruation.Confidence)
	}
	start, _ := time.Parse(time.DateOnly, p.NextMenstruation.WindowStart)
	end, _ := time.Parse(time.DateOnly, p.NextMenstruation.WindowEnd)
	if days := int(end.Sub(start).Hours() / 24); days < 10 {
		t.Fatalf("window width = %d days, want wide (>=10) for variability %d", days, p.Basis.VariabilityDays)
	}
}

func TestEstimateFiltersImplausibleIntervals(t *testing.T) {
	// A 120-day gap (possible in PCOS/perimenopause, valid stored data) is
	// excluded from the estimate rather than treated as an error (S06, S11).
	p := Estimate(dates(
		"2025-06-01", "2025-09-29", // 120-day gap: filtered
		"2025-10-28", "2025-11-26", "2025-12-25",
	), now)
	if p.Basis.MedianLengthDays != 29 && p.Basis.MedianLengthDays != 30 {
		t.Fatalf("median = %d, want ~29-30 after filtering the 120-day gap", p.Basis.MedianLengthDays)
	}
}
