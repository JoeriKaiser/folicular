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
	LinkID string `json:"link_id"`
	Role   string `json:"role"`
	AsOf   string `json:"as_of"`
	// Sealed by the tracker's device under the Duo link key. The server
	// relays it and cannot read it. Null until the tracker has published one.
	Payload         []byte                `json:"payload"`
	PayloadUpdatedAt *string              `json:"payload_updated_at"`
	SupportRequests *[]supportRequestView `json:"support_requests"`
}

type supportRequestView struct {
	ID         string `json:"id"`
	AuthorRole string `json:"author_role"`
	Kind       string `json:"kind"`
	// Sealed under the Duo link key like the projection itself.
	MessageCiphertext []byte  `json:"message_ciphertext"`
	CreatedAt         string  `json:"created_at"`
	AcknowledgedAt    *string `json:"acknowledged_at"`
}

// HandleDuoView returns the sealed partner projection.
//
// The server no longer composes this view: it cannot read cycle starts or
// daily entries. The tracker's device applies the grants locally, seals the
// result under the Duo link key, and publishes it via HandlePutDuoPayload.
// Grants are therefore enforced at composition time, which is strictly
// stronger than server-side filtering - the server cannot leak what it never
// received.
func (d *Deps) HandleDuoView(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	ctx := r.Context()
	now := time.Now().UTC()

	link, role, ok := d.activeLinkFor(ctx, accountID)
	if !ok {
		problem.Write(w, r, problem.Status(http.StatusNotFound, "Aucun lien actif",
			"Aucun lien Duo actif pour ce compte."))
		return
	}

	view := duoView{LinkID: link.ID, Role: role, AsOf: now.Format(time.RFC3339)}

	if payload, err := d.Q.GetDuoPayload(ctx, link.ID); err == nil {
		view.Payload = payload.Ciphertext
		updated := payload.UpdatedAt
		view.PayloadUpdatedAt = &updated
	} else if !isNotFound(err) {
		d.Log.Error("duo payload lookup failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	// The support thread is sealed message-by-message; both members receive
	// it and decrypt what they can.
	thread, err := d.supportThread(ctx, link.ID)
	if err != nil {
		d.Log.Error("support thread failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	view.SupportRequests = &thread

	writeJSON(w, http.StatusOK, view)
}

// activeLinkFor resolves the caller's active link, partner first then tracker.
func (d *Deps) activeLinkFor(ctx context.Context, accountID string) (dbgen.DuoLink, string, bool) {
	if link, err := d.Q.GetActiveDuoLinkByPartner(ctx, sql.NullString{String: accountID, Valid: true}); err == nil {
		return link, domain.DuoRolePartner, true
	}
	if link, err := d.Q.GetActiveDuoLinkByOwner(ctx, accountID); err == nil {
		return link, domain.DuoRoleTracker, true
	}
	return dbgen.DuoLink{}, "", false
}

type putDuoPayloadRequest struct {
	Payload []byte `json:"payload"`
}

// maxDuoPayloadBytes bounds one sealed projection.
const maxDuoPayloadBytes = 64 * 1024

// HandlePutDuoPayload lets the tracker publish the sealed projection for their
// link. Only the tracker may publish: the projection is derived from their
// records.
func (d *Deps) HandlePutDuoPayload(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	ctx := r.Context()

	link, role, ok := d.activeLinkFor(ctx, accountID)
	if !ok {
		problem.Write(w, r, problem.Status(http.StatusNotFound, "Aucun lien actif",
			"Aucun lien Duo actif pour ce compte."))
		return
	}
	if role != domain.DuoRoleTracker {
		problem.Write(w, r, problem.Status(http.StatusForbidden, "Accès refusé",
			"Seul le suivi principal peut publier la projection Duo."))
		return
	}

	var req putDuoPayloadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Payload) == 0 {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"payload: requis"))
		return
	}
	if len(req.Payload) > maxDuoPayloadBytes {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"payload: charge utile trop volumineuse"))
		return
	}

	if err := d.Q.UpsertDuoPayload(ctx, dbgen.UpsertDuoPayloadParams{
		LinkID:     link.ID,
		Ciphertext: req.Payload,
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		d.Log.Error("duo payload upsert failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
			MessageCiphertext: row.MessageCiphertext,
			CreatedAt:         row.CreatedAt,
			AcknowledgedAt:    sp(row.AcknowledgedAt),
		})
	}
	return out, nil
}

// Support requests ---------------------------------------------------------

type createSupportRequest struct {
	LinkID string `json:"link_id"`
	Kind   string `json:"kind"`
	// Sealed under the Duo link key by the sender's device.
	Message []byte `json:"message"`
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
		MessageCiphertext: req.Message, CreatedAt: now,
	}); err != nil {
		d.Log.Error("support request insert failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	writeJSON(w, http.StatusCreated, supportRequestView{
		ID: id, AuthorRole: role, Kind: req.Kind, MessageCiphertext: req.Message, CreatedAt: now,
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
