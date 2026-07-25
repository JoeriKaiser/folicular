package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"folicular/internal/api/problem"
	"folicular/internal/auth"
	"folicular/internal/cyclecalc"
	"folicular/internal/db/dbgen"
	"folicular/internal/domain"
)

// Duo: purpose-designed partner surface. Sharing is private by default,
// explicit, granular (per-field grants), visible, and reversible. Pairing
// happens through a short-lived, single-use code transported as a
// human-typed value, a shareable link, or a QR code (the client renders the
// QR from pairing_url; the server never needs QR logic).

const duoInvitationTTL = 7 * 24 * time.Hour

type invitationResponse struct {
	LinkID      string `json:"link_id"`
	PairingCode string `json:"pairing_code"`
	PairingURL  string `json:"pairing_url"`
	ExpiresAt   string `json:"expires_at"`
}

// HandleCreateInvitation creates a pending Duo link and returns the pairing
// code plus a link the client can share or render as a QR code. The code in
// the URL is a bearer-like secret: single use, 7-day expiry, rate-limited
// acceptance.
func (d *Deps) HandleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	now := time.Now().UTC()

	display, codeHash, err := auth.GeneratePairingCode()
	if err != nil {
		d.Log.Error("pairing code generation failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	linkID := uuid.NewString()
	nowS := now.Format(time.RFC3339)
	if err := d.Q.InsertDuoLink(r.Context(), dbgen.InsertDuoLinkParams{
		ID:             linkID,
		OwnerAccountID: accountID,
		CodeHash:       codeHash,
		CreatedAt:      nowS,
		UpdatedAt:      nowS,
	}); err != nil {
		d.Log.Error("duo link insert failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	writeJSON(w, http.StatusCreated, invitationResponse{
		LinkID:      linkID,
		PairingCode: display,
		PairingURL:  d.PairingBaseURL + "/accept?code=" + url.QueryEscape(display),
		ExpiresAt:   now.Add(duoInvitationTTL).Format(time.RFC3339),
	})
}

type acceptRequest struct {
	PairingCode string `json:"pairing_code"`
}

type linkView struct {
	ID        string  `json:"id"`
	Role      string  `json:"role"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
	RevokedAt *string `json:"revoked_at"`
}

// HandleAcceptLink lets a partner (who has their own anonymous account)
// accept a pending invitation. Generic errors prevent code enumeration.
func (d *Deps) HandleAcceptLink(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())

	var req acceptRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	normalized := auth.NormalizeCode(req.PairingCode)
	link, err := d.Q.GetPendingDuoLinkByCodeHash(r.Context(), auth.HashCode(normalized))
	if err != nil {
		problem.Write(w, r, problem.Status(http.StatusNotFound, "Code invalide",
			"Code de partage invalide, déjà utilisé ou expiré."))
		return
	}
	created, err := time.Parse(time.RFC3339, link.CreatedAt)
	if err != nil || time.Since(created) > duoInvitationTTL {
		problem.Write(w, r, problem.Status(http.StatusNotFound, "Code invalide",
			"Code de partage invalide, déjà utilisé ou expiré."))
		return
	}
	if link.OwnerAccountID == accountID {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"Impossible de créer un lien Duo avec votre propre compte."))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.Q.AcceptDuoLink(r.Context(), dbgen.AcceptDuoLinkParams{
		PartnerAccountID: ns(stringPtr(accountID)),
		UpdatedAt:        now,
		ID:               link.ID,
	}); err != nil {
		d.Log.Error("duo link accept failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"link": linkView{
		ID: link.ID, Role: domain.DuoRolePartner, Status: "active", CreatedAt: link.CreatedAt,
	}})
}

// HandleListLinks lists the caller's Duo links in both roles.
func (d *Deps) HandleListLinks(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	rows, err := d.Q.ListDuoLinksForAccount(r.Context(), dbgen.ListDuoLinksForAccountParams{
		OwnerAccountID:   accountID,
		PartnerAccountID: sql.NullString{String: accountID, Valid: true},
	})
	if err != nil {
		d.Log.Error("duo link list failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	out := make([]linkView, 0, len(rows))
	for _, row := range rows {
		role := domain.DuoRolePartner
		if row.OwnerAccountID == accountID {
			role = domain.DuoRoleTracker
		}
		out = append(out, linkView{
			ID: row.ID, Role: role, Status: row.Status,
			CreatedAt: row.CreatedAt, RevokedAt: sp(row.RevokedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

type patchGrantRequest struct {
	Field   string `json:"field"`
	Granted bool   `json:"granted"`
}

// HandlePatchGrants lets the tracker grant or revoke one sharing field.
// Revocation is immediately observable by the partner on their next view.
func (d *Deps) HandlePatchGrants(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	linkID := strings.TrimSpace(chi.URLParam(r, "linkID"))

	link, err := d.Q.GetDuoLinkByID(r.Context(), linkID)
	if err != nil {
		problem.Write(w, r, problem.Status(http.StatusNotFound, "Introuvable", "Lien Duo inconnu."))
		return
	}
	if link.OwnerAccountID != accountID {
		problem.Write(w, r, problem.Status(http.StatusForbidden, "Interdit",
			"Seul le suivi principal gère les partages."))
		return
	}
	if link.Status != "active" {
		problem.Write(w, r, problem.Status(http.StatusConflict, "Lien inactif",
			"Ce lien Duo n'est pas actif."))
		return
	}

	var req patchGrantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !isOneOf(req.Field, domain.DuoGrantFields) {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"field: valeur invalide"))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if req.Granted {
		err = d.Q.UpsertGrant(r.Context(), dbgen.UpsertGrantParams{
			ID: uuid.NewString(), LinkID: linkID, Field: req.Field, GrantedAt: now,
		})
	} else {
		err = d.Q.RevokeGrant(r.Context(), dbgen.RevokeGrantParams{
			RevokedAt: ns(stringPtr(now)), LinkID: linkID, Field: req.Field,
		})
	}
	if err != nil {
		d.Log.Error("grant update failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleRevokeLink ends a link. Either member may do so; grants stop
// applying immediately and no history is transferred.
func (d *Deps) HandleRevokeLink(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	linkID := strings.TrimSpace(chi.URLParam(r, "linkID"))

	link, err := d.Q.GetDuoLinkByID(r.Context(), linkID)
	if err != nil {
		problem.Write(w, r, problem.Status(http.StatusNotFound, "Introuvable", "Lien Duo inconnu."))
		return
	}
	if link.OwnerAccountID != accountID && !strEquals(link.PartnerAccountID, accountID) {
		problem.Write(w, r, problem.Status(http.StatusForbidden, "Interdit",
			"Vous n'êtes pas membre de ce lien Duo."))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	n, err := d.Q.RevokeDuoLink(r.Context(), dbgen.RevokeDuoLinkParams{
		RevokedAt: ns(stringPtr(now)), UpdatedAt: now, ID: linkID,
	})
	if err != nil {
		d.Log.Error("duo link revoke failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	if n == 0 {
		problem.Write(w, r, problem.Status(http.StatusConflict, "Lien inactif",
			"Ce lien Duo n'est plus actif."))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Duo view: the partner-facing projection, built strictly from active
// grants. The tracker role sees only the support thread. Ungranted fields
// are null - absence is deliberately indistinguishable from no data.

type duoView struct {
	LinkID          string              `json:"link_id"`
	Role            string              `json:"role"`
	AsOf            string              `json:"as_of"`
	CycleDay        *int                `json:"cycle_day"`
	PeriodEstimate  *periodEstimateView `json:"period_estimate"`
	Mood            *sharedLevel        `json:"mood"`
	Energy          *sharedLevel        `json:"energy"`
	SupportRequests *[]supportRequestView `json:"support_requests"`
}

type periodEstimateView struct {
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
}

type sharedLevel struct {
	Date  string `json:"date"`
	Level int    `json:"level"`
}

type supportRequestView struct {
	ID             string  `json:"id"`
	AuthorRole     string  `json:"author_role"`
	Kind           string  `json:"kind"`
	Message        string  `json:"message"`
	CreatedAt      string  `json:"created_at"`
	AcknowledgedAt *string `json:"acknowledged_at"`
}

// HandleDuoView serves the projection. Private notes and raw observations
// never pass through this endpoint.
func (d *Deps) HandleDuoView(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	ctx := r.Context()
	now := time.Now().UTC()

	// Partner first, then tracker.
	link, err := d.Q.GetActiveDuoLinkByPartner(ctx, sql.NullString{String: accountID, Valid: true})
	role := domain.DuoRolePartner
	var ownerID string
	if err != nil {
		ownerLink, oerr := d.Q.GetActiveDuoLinkByOwner(ctx, accountID)
		if oerr != nil {
			problem.Write(w, r, problem.Status(http.StatusNotFound, "Aucun lien actif",
				"Aucun lien Duo actif pour ce compte."))
			return
		}
		link = ownerLink
		role = domain.DuoRoleTracker
		ownerID = ownerLink.OwnerAccountID
	} else {
		ownerID = link.OwnerAccountID
	}

	view := duoView{LinkID: link.ID, Role: role, AsOf: now.Format(time.RFC3339)}

	// The support thread is visible to the tracker unconditionally (it is
	// their link); the partner sees it only with the grant.
	thread, err := d.supportThread(ctx, link.ID)
	if err != nil {
		d.Log.Error("support thread failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	if role == domain.DuoRoleTracker {
		view.SupportRequests = &thread
		writeJSON(w, http.StatusOK, view)
		return
	}

	grants, err := d.Q.ListActiveGrantsByLink(ctx, link.ID)
	if err != nil {
		d.Log.Error("grant list failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	granted := make(map[string]bool, len(grants))
	for _, g := range grants {
		granted[g.Field] = true
	}

	if granted["cycle_day"] || granted["period_estimate"] {
		starts := d.ownerCycleStarts(ctx, ownerID)
		if granted["cycle_day"] {
			view.CycleDay = cycleDayFor(starts, now.Format(time.DateOnly))
		}
		if granted["period_estimate"] {
			pred := cyclecalc.Estimate(parseStarts(starts), now)
			if pred.NextMenstruation != nil {
				view.PeriodEstimate = &periodEstimateView{
					WindowStart: pred.NextMenstruation.WindowStart,
					WindowEnd:   pred.NextMenstruation.WindowEnd,
				}
			}
		}
	}

	if granted["mood"] || granted["energy"] {
		entry := d.latestDailyEntry(ctx, ownerID, now)
		if entry != nil {
			if granted["mood"] && entry.MoodLevel != nil {
				view.Mood = &sharedLevel{Date: entry.EntryDate, Level: *entry.MoodLevel}
			}
			if granted["energy"] && entry.EnergyLevel != nil {
				view.Energy = &sharedLevel{Date: entry.EntryDate, Level: *entry.EnergyLevel}
			}
		}
	}

	if granted["support_requests"] {
		view.SupportRequests = &thread
	}

	writeJSON(w, http.StatusOK, view)
}

func (d *Deps) supportThread(ctx context.Context, linkID string) ([]supportRequestView, error) {
	rows, err := d.Q.ListSupportRequestsByLink(ctx, dbgen.ListSupportRequestsByLinkParams{
		LinkID: linkID, Limit: 50,
	})
	if err != nil {
		return nil, err
	}
	out := make([]supportRequestView, 0, len(rows))
	for _, row := range rows {
		out = append(out, supportRequestView{
			ID: row.ID, AuthorRole: row.AuthorRole, Kind: row.Kind,
			Message: row.Message, CreatedAt: row.CreatedAt, AcknowledgedAt: sp(row.AcknowledgedAt),
		})
	}
	return out, nil
}

func (d *Deps) ownerCycleStarts(ctx context.Context, ownerID string) []string {
	rows, err := d.Q.ListCycleStarts(ctx, ownerID)
	if err != nil {
		return nil
	}
	return rows
}

func parseStarts(starts []string) []time.Time {
	out := make([]time.Time, 0, len(starts))
	for _, s := range starts {
		if t, err := domain.ParseDate(s); err == nil {
			out = append(out, t)
		}
	}
	return out
}

// latestDailyEntry returns the owner's most recent daily entry within the
// last 60 days, or nil.
func (d *Deps) latestDailyEntry(ctx context.Context, ownerID string, now time.Time) *domain.DailyEntry {
	rows, err := d.Q.ListDailyEntriesByRange(ctx, dbgen.ListDailyEntriesByRangeParams{
		AccountID:  ownerID,
		EntryDate:  now.AddDate(0, 0, -60).Format(time.DateOnly),
		EntryDate_2: now.Format(time.DateOnly),
	})
	if err != nil || len(rows) == 0 {
		return nil
	}
	e := rowToDailyEntry(rows[len(rows)-1])
	return &e
}

// Support requests ---------------------------------------------------------

type createSupportRequest struct {
	LinkID  string `json:"link_id"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// HandleCreateSupportRequest lets either member ask for support.
func (d *Deps) HandleCreateSupportRequest(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())

	var req createSupportRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !isOneOf(req.Kind, domain.DuoSupportKinds) {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"kind: valeur invalide"))
		return
	}
	if len(req.Message) > 1000 {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"message: dépasse la longueur maximale"))
		return
	}
	link, err := d.Q.GetDuoLinkByID(r.Context(), req.LinkID)
	if err != nil || link.Status != "active" {
		problem.Write(w, r, problem.Status(http.StatusNotFound, "Introuvable", "Lien Duo actif introuvable."))
		return
	}
	role, ok := memberRole(link, accountID)
	if !ok {
		problem.Write(w, r, problem.Status(http.StatusForbidden, "Interdit",
			"Vous n'êtes pas membre de ce lien Duo."))
		return
	}

	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.Q.InsertSupportRequest(r.Context(), dbgen.InsertSupportRequestParams{
		ID: id, LinkID: link.ID, AuthorRole: role, Kind: req.Kind,
		Message: req.Message, CreatedAt: now,
	}); err != nil {
		d.Log.Error("support request insert failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	writeJSON(w, http.StatusCreated, supportRequestView{
		ID: id, AuthorRole: role, Kind: req.Kind, Message: req.Message, CreatedAt: now,
	})
}

// HandleAckSupportRequest lets the recipient acknowledge a request.
func (d *Deps) HandleAckSupportRequest(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))

	sr, err := d.Q.GetSupportRequestByID(r.Context(), requestID)
	if err != nil {
		problem.Write(w, r, problem.Status(http.StatusNotFound, "Introuvable", "Demande de soutien introuvable."))
		return
	}
	link, err := d.Q.GetDuoLinkByID(r.Context(), sr.LinkID)
	if err != nil {
		problem.Write(w, r, problem.Status(http.StatusNotFound, "Introuvable", "Lien Duo inconnu."))
		return
	}
	role, ok := memberRole(link, accountID)
	if !ok || role == sr.AuthorRole {
		problem.Write(w, r, problem.Status(http.StatusForbidden, "Interdit",
			"Seul le destinataire peut acquitter cette demande."))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.Q.AckSupportRequest(r.Context(), dbgen.AckSupportRequestParams{
		AcknowledgedAt: ns(stringPtr(now)), ID: requestID,
	}); err != nil {
		d.Log.Error("support request ack failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Helpers ------------------------------------------------------------------

func isOneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func memberRole(link dbgen.DuoLink, accountID string) (string, bool) {
	if link.OwnerAccountID == accountID {
		return domain.DuoRoleTracker, true
	}
	if strEquals(link.PartnerAccountID, accountID) {
		return domain.DuoRolePartner, true
	}
	return "", false
}

func strEquals(v sql.NullString, s string) bool { return v.Valid && v.String == s }
