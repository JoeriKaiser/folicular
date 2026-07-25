package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"folicular/internal/api/problem"
	"folicular/internal/auth"
	"folicular/internal/db/dbgen"
	"folicular/internal/domain"
)

// Sync protocol: see docs/api.md.
//
// Record content is end-to-end encrypted on the client and opaque here (see the
// client repo's docs/architecture/E2EE_DESIGN.md). This server therefore
// validates only the routing envelope - identifiers, entity type, ordering
// timestamp, tombstone flag - and never the payload. Content validation is the
// client's responsibility because it is the only party that can read it.
//
// Each pushed change is applied with an entity-level last-write-wins guard and
// appended to the account's change log. Losing records are returned as
// conflicts carrying the server's current sealed state, so nothing is silently
// lost: the client decrypts and reconciles locally.

type pushChange struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	ClientRev  string `json:"client_rev"`
	UpdatedAt  string `json:"updated_at"`
	Deleted    bool   `json:"deleted"`
	Ciphertext []byte `json:"ciphertext"`
}

type pushRequest struct {
	Changes []pushChange `json:"changes"`
}

type appliedRef struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Seq        int64  `json:"seq"`
}

type rejectedRef struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id,omitempty"`
	Detail     string `json:"detail"`
}

type conflictRef struct {
	EntityType        string `json:"entity_type"`
	EntityID          string `json:"entity_id"`
	Reason            string `json:"reason"`
	CurrentClientRev  string `json:"current_client_rev"`
	CurrentUpdatedAt  string `json:"current_updated_at"`
	CurrentDeleted    bool   `json:"current_deleted"`
	CurrentCiphertext []byte `json:"current_ciphertext"`
}

type pushResponse struct {
	Applied   []appliedRef  `json:"applied"`
	Rejected  []rejectedRef `json:"rejected"`
	Conflicts []conflictRef `json:"conflicts"`
	Cursor    int64         `json:"cursor"`
}

const maxBatchChanges = 1000

// maxCiphertextBytes bounds a single sealed record. Generous relative to any
// realistic observation, tight enough that the store cannot be repurposed as
// arbitrary blob hosting.
const maxCiphertextBytes = 64 * 1024

// HandleSyncPush applies a batch of client changes.
func (d *Deps) HandleSyncPush(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())

	var req pushRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Changes) == 0 {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée", "changes: lot vide"))
		return
	}
	if len(req.Changes) > maxBatchChanges {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			fmt.Sprintf("changes: au plus %d changements par lot", maxBatchChanges)))
		return
	}

	resp := pushResponse{
		Applied:   []appliedRef{},
		Rejected:  []rejectedRef{},
		Conflicts: []conflictRef{},
	}

	for i, ch := range req.Changes {
		applied, conflict, rej := d.applyChange(r.Context(), accountID, ch)
		switch {
		case rej != nil:
			rej.Detail = fmt.Sprintf("changes[%d]: %s", i, rej.Detail)
			resp.Rejected = append(resp.Rejected, *rej)
		case conflict != nil:
			resp.Conflicts = append(resp.Conflicts, *conflict)
		default:
			resp.Applied = append(resp.Applied, *applied)
		}
	}

	cursor, err := d.Q.LatestCursor(r.Context(), accountID)
	if err != nil && !isNotFound(err) {
		d.Log.Error("cursor lookup failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	resp.Cursor = cursor // zero when the account has no changes yet
	writeJSON(w, http.StatusOK, resp)
}

// validate checks the routing envelope only. The payload is ciphertext and is
// deliberately not inspected.
func (ch pushChange) validate() error {
	if !domain.IsEntityType(ch.EntityType) {
		return fmt.Errorf("entity_type: type inconnu %q", ch.EntityType)
	}
	if !domain.IsUUID(ch.EntityID) {
		return fmt.Errorf("entity_id: UUID invalide")
	}
	if !domain.IsUUID(ch.ClientRev) {
		return fmt.Errorf("client_rev: UUID invalide")
	}
	if !domain.IsInstant(ch.UpdatedAt) {
		return fmt.Errorf("updated_at: instant RFC 3339 invalide")
	}
	if t, err := time.Parse(time.RFC3339, ch.UpdatedAt); err == nil {
		if t.After(time.Now().Add(5 * time.Minute)) {
			return fmt.Errorf("updated_at: horodatage trop éloigné dans le futur (horloge décalée)")
		}
	}
	if len(ch.Ciphertext) > maxCiphertextBytes {
		return fmt.Errorf("ciphertext: au plus %d octets", maxCiphertextBytes)
	}
	if !ch.Deleted && len(ch.Ciphertext) == 0 {
		return fmt.Errorf("ciphertext: requis sauf pour une suppression")
	}
	return nil
}

// applyChange applies one change in its own transaction: the record upsert and
// the change-log append are atomic per change. Losing the last-write-wins guard
// rolls back and returns the server's current sealed state.
func (d *Deps) applyChange(ctx context.Context, accountID string, ch pushChange) (*appliedRef, *conflictRef, *rejectedRef) {
	reject := func(entityID, detail string) (*appliedRef, *conflictRef, *rejectedRef) {
		return nil, nil, &rejectedRef{EntityType: ch.EntityType, EntityID: entityID, Detail: detail}
	}

	if err := ch.validate(); err != nil {
		return reject(ch.EntityID, err.Error())
	}

	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		d.Log.Error("begin tx failed", "err", err)
		return reject(ch.EntityID, "transaction impossible")
	}
	td := &Deps{Q: d.Q.WithTx(tx), DB: d.DB, Log: d.Log}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	applied, conflict, rej := td.applySealedRecord(ctx, accountID, ch, reject)
	if applied == nil {
		return applied, conflict, rej
	}
	if err := tx.Commit(); err != nil {
		d.Log.Error("commit failed", "err", err)
		return reject(applied.EntityID, "échec de l'enregistrement serveur")
	}
	committed = true
	return applied, conflict, rej
}

// applySealedRecord upserts one sealed record and appends it to the change log,
// against the transaction-bound queries.
func (d *Deps) applySealedRecord(
	ctx context.Context,
	accountID string,
	ch pushChange,
	reject func(entityID, detail string) (*appliedRef, *conflictRef, *rejectedRef),
) (*appliedRef, *conflictRef, *rejectedRef) {
	now := time.Now().UTC().Format(time.RFC3339)

	deleted := int64(0)
	var ciphertext []byte
	if ch.Deleted {
		deleted = 1 // a tombstone carries no content
	} else {
		ciphertext = ch.Ciphertext
	}

	n, err := d.Q.UpsertRecord(ctx, dbgen.UpsertRecordParams{
		AccountID:  accountID,
		EntityID:   ch.EntityID,
		EntityType: ch.EntityType,
		ClientRev:  ch.ClientRev,
		Ciphertext: ciphertext,
		Deleted:    deleted,
		UpdatedAt:  ch.UpdatedAt,
		RecordedAt: now,
	})
	if err != nil {
		d.Log.Error("record upsert failed", "entity_type", ch.EntityType, "err", err)
		return reject(ch.EntityID, "échec de l'enregistrement serveur")
	}

	if n == 0 {
		// Lost the last-write-wins guard: hand back the server's current
		// sealed state so the client can decrypt and reconcile.
		cur, err := d.Q.GetRecord(ctx, dbgen.GetRecordParams{
			AccountID: accountID,
			EntityID:  ch.EntityID,
		})
		if err != nil {
			d.Log.Error("conflict lookup failed", "entity_id", ch.EntityID, "err", err)
			return reject(ch.EntityID, "état serveur indisponible")
		}
		return nil, &conflictRef{
			EntityType:        cur.EntityType,
			EntityID:          cur.EntityID,
			Reason:            "superseded",
			CurrentClientRev:  cur.ClientRev,
			CurrentUpdatedAt:  cur.UpdatedAt,
			CurrentDeleted:    cur.Deleted == 1,
			CurrentCiphertext: cur.Ciphertext,
		}, nil
	}

	seq, err := d.Q.RecordChange(ctx, dbgen.RecordChangeParams{
		AccountID:  accountID,
		EntityType: ch.EntityType,
		EntityID:   ch.EntityID,
		ClientRev:  ch.ClientRev,
		Deleted:    deleted,
		Ciphertext: ciphertext,
		UpdatedAt:  ch.UpdatedAt,
		RecordedAt: now,
	})
	if err != nil {
		d.Log.Error("change log append failed", "entity_type", ch.EntityType, "id", ch.EntityID, "err", err)
		return reject(ch.EntityID, "échec de l'enregistrement serveur")
	}

	return &appliedRef{EntityType: ch.EntityType, EntityID: ch.EntityID, Seq: seq}, nil, nil
}

// Pull --------------------------------------------------------------------

type pullChange struct {
	Seq        int64  `json:"seq"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	ClientRev  string `json:"client_rev"`
	Deleted    bool   `json:"deleted"`
	UpdatedAt  string `json:"updated_at"`
	Ciphertext []byte `json:"ciphertext"`
}

type pullResponse struct {
	Changes []pullChange `json:"changes"`
	Cursor  int64        `json:"cursor"`
	HasMore bool         `json:"has_more"`
}

const (
	defaultPullLimit = 500
	maxPullLimit     = 2000
)

// HandleSyncPull returns ordered changes since the client's cursor.
func (d *Deps) HandleSyncPull(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())

	since := int64(0)
	if v := r.URL.Query().Get("since"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil || parsed < 0 {
			problem.Write(w, r, problem.Status(http.StatusBadRequest, "Requête invalide", "since: entier positif attendu"))
			return
		}
		since = parsed
	}
	limit := int64(defaultPullLimit)
	if v := r.URL.Query().Get("limit"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil || parsed < 1 {
			problem.Write(w, r, problem.Status(http.StatusBadRequest, "Requête invalide", "limit: entier positif attendu"))
			return
		}
		if parsed > maxPullLimit {
			parsed = maxPullLimit
		}
		limit = parsed
	}

	rows, err := d.Q.PullChanges(r.Context(), dbgen.PullChangesParams{
		AccountID: accountID,
		Seq:       since,
		Limit:     limit,
	})
	if err != nil {
		d.Log.Error("pull failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	resp := pullResponse{Changes: make([]pullChange, 0, len(rows)), Cursor: since}
	for _, row := range rows {
		pc := pullChange{
			Seq:        row.Seq,
			EntityType: row.EntityType,
			EntityID:   row.EntityID,
			ClientRev:  row.ClientRev,
			Deleted:    row.Deleted == 1,
			UpdatedAt:  row.UpdatedAt,
		}
		if !pc.Deleted {
			pc.Ciphertext = row.Ciphertext
		}
		resp.Changes = append(resp.Changes, pc)
		resp.Cursor = row.Seq
	}
	resp.HasMore = int64(len(rows)) == limit
	writeJSON(w, http.StatusOK, resp)
}
