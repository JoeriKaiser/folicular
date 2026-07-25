package domain

// Duo vocabularies. Sharing is private by default, explicit, granular,
// visible, and reversible; these enums are the grant surface.

// DuoGrantFields are the per-field sharing grants a tracker can give a
// partner. Mirrors the Android client's DuoSharingField.
var DuoGrantFields = []string{
	"cycle_day", "period_estimate", "mood", "energy", "support_requests",
}

// DuoSupportKinds classify support requests without stereotyping.
var DuoSupportKinds = []string{"general", "comfort", "practical", "space"}

// DuoRoles within a link.
const (
	DuoRoleTracker = "tracker"
	DuoRolePartner = "partner"
)
