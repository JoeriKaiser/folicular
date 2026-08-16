package domain

// Life stages aligned with STRAW+10 (S11). User-selected, never inferred.
// Preserved as canonical research-backed domain vocabulary.
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
// (S20-S25). Preserved as canonical research-backed domain vocabulary.
var TrackingFocuses = []string{"pms", "pmdd", "endometriosis", "pcos", "custom"}
