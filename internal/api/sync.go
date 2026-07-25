package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"folicular/internal/api/problem"
	"folicular/internal/auth"
	"folicular/internal/db/dbgen"
	"folicular/internal/domain"
)

// Sync protocol: see docs/api.md. Each pushed change is validated, applied
// with an entity-level last-write-wins guard, and appended to the account's
// change log. Losing records are returned as conflicts carrying the server's
// current state, so nothing is silently lost.

type pushChange struct {
	EntityType string          `json:"entity_type"`
	Data       json.RawMessage `json:"data"`
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
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Reason     string          `json:"reason"`
	Current    json.RawMessage `json:"current"`
}

type pushResponse struct {
	Applied   []appliedRef  `json:"applied"`
	Rejected  []rejectedRef `json:"rejected"`
	Conflicts []conflictRef `json:"conflicts"`
	Cursor    int64         `json:"cursor"`
}

const maxBatchChanges = 1000

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

// applyChange validates and applies one change in its own transaction: the
// entity upsert and the change-log append are atomic per change. Conflicts
// (LWW guard lost) roll back and return the server's current state.
func (d *Deps) applyChange(ctx context.Context, accountID string, ch pushChange) (*appliedRef, *conflictRef, *rejectedRef) {
	reject := func(entityID, detail string) (*appliedRef, *conflictRef, *rejectedRef) {
		return nil, nil, &rejectedRef{EntityType: ch.EntityType, EntityID: entityID, Detail: detail}
	}

	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		d.Log.Error("begin tx failed", "err", err)
		return reject("", "transaction impossible")
	}
	td := &Deps{Q: d.Q.WithTx(tx), DB: d.DB, Log: d.Log}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	applied, conflict, rej := td.dispatchChange(ctx, accountID, ch, reject)
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

// dispatchChange decodes, validates, applies (LWW), and records one change
// against the transaction-bound queries.
func (d *Deps) dispatchChange(
	ctx context.Context,
	accountID string,
	ch pushChange,
	reject func(entityID, detail string) (*appliedRef, *conflictRef, *rejectedRef),
) (*appliedRef, *conflictRef, *rejectedRef) {
	switch ch.EntityType {
	case domain.TypeCycle:
		return applyRecord(ctx, d, accountID, ch, reject,
			func(c domain.Cycle) (domain.Envelope, error) {
				if err := c.Validate(); err != nil {
					return c.Envelope, err
				}
				return c.Envelope, d.upsertCycle(ctx, accountID, c)
			},
			func(id string) (any, error) {
				row, err := d.Q.GetCycleByID(ctx, dbgen.GetCycleByIDParams{ID: id, AccountID: accountID})
				if err != nil {
					return nil, err
				}
				return rowToCycle(row), nil
			},
		)
	case domain.TypeBleedingObservation:
		return applyRecord(ctx, d, accountID, ch, reject,
			func(b domain.BleedingObservation) (domain.Envelope, error) {
				if err := b.Validate(); err != nil {
					return b.Envelope, err
				}
				return b.Envelope, d.upsertBleeding(ctx, accountID, b)
			},
			func(id string) (any, error) {
				row, err := d.Q.GetBleedingObservationByID(ctx, dbgen.GetBleedingObservationByIDParams{ID: id, AccountID: accountID})
				if err != nil {
					return nil, err
				}
				return rowToBleeding(row), nil
			},
		)
	case domain.TypeDailyEntry:
		return applyRecord(ctx, d, accountID, ch, reject,
			func(e domain.DailyEntry) (domain.Envelope, error) {
				if err := e.Validate(); err != nil {
					return e.Envelope, err
				}
				return e.Envelope, d.upsertDailyEntry(ctx, accountID, e)
			},
			func(id string) (any, error) {
				row, err := d.Q.GetDailyEntryByID(ctx, dbgen.GetDailyEntryByIDParams{ID: id, AccountID: accountID})
				if err != nil {
					return nil, err
				}
				return rowToDailyEntry(row), nil
			},
		)
	case domain.TypeSymptomDefinition:
		return applyRecord(ctx, d, accountID, ch, reject,
			func(s domain.SymptomDefinition) (domain.Envelope, error) {
				if err := s.Validate(); err != nil {
					return s.Envelope, err
				}
				return s.Envelope, d.upsertSymptomDef(ctx, accountID, s)
			},
			func(id string) (any, error) {
				row, err := d.Q.GetSymptomDefinitionByID(ctx, dbgen.GetSymptomDefinitionByIDParams{ID: id, AccountID: accountID})
				if err != nil {
					return nil, err
				}
				return rowToSymptomDef(row), nil
			},
		)
	case domain.TypeSymptomLog:
		return applyRecord(ctx, d, accountID, ch, reject,
			func(s domain.SymptomLog) (domain.Envelope, error) {
				if err := s.Validate(); err != nil {
					return s.Envelope, err
				}
				return s.Envelope, d.upsertSymptomLog(ctx, accountID, s)
			},
			func(id string) (any, error) {
				row, err := d.Q.GetSymptomLogByID(ctx, dbgen.GetSymptomLogByIDParams{ID: id, AccountID: accountID})
				if err != nil {
					return nil, err
				}
				return rowToSymptomLog(row), nil
			},
		)
	case domain.TypeBiomarkerObservation:
		return applyRecord(ctx, d, accountID, ch, reject,
			func(b domain.BiomarkerObservation) (domain.Envelope, error) {
				if err := b.Validate(); err != nil {
					return b.Envelope, err
				}
				return b.Envelope, d.upsertBiomarker(ctx, accountID, b)
			},
			func(id string) (any, error) {
				row, err := d.Q.GetBiomarkerObservationByID(ctx, dbgen.GetBiomarkerObservationByIDParams{ID: id, AccountID: accountID})
				if err != nil {
					return nil, err
				}
				return rowToBiomarker(row), nil
			},
		)
	case domain.TypeMedicationLog:
		return applyRecord(ctx, d, accountID, ch, reject,
			func(m domain.MedicationLog) (domain.Envelope, error) {
				if err := m.Validate(); err != nil {
					return m.Envelope, err
				}
				return m.Envelope, d.upsertMedication(ctx, accountID, m)
			},
			func(id string) (any, error) {
				row, err := d.Q.GetMedicationLogByID(ctx, dbgen.GetMedicationLogByIDParams{ID: id, AccountID: accountID})
				if err != nil {
					return nil, err
				}
				return rowToMedication(row), nil
			},
		)
	default:
		return reject("", "entity_type: valeur invalide "+strconv.Quote(ch.EntityType))
	}
}

// applyRecord decodes, validates, applies (LWW), and records one change.
// T is the domain record type; validateApply validates then upserts,
// returning errNotApplied when the LWW guard rejects the write;
// fetchCurrent loads the server state for conflicts.
func applyRecord[T any](
	ctx context.Context,
	d *Deps,
	accountID string,
	ch pushChange,
	reject func(entityID, detail string) (*appliedRef, *conflictRef, *rejectedRef),
	validateApply func(T) (domain.Envelope, error),
	fetchCurrent func(id string) (any, error),
) (*appliedRef, *conflictRef, *rejectedRef) {
	var record T
	if err := json.Unmarshal(ch.Data, &record); err != nil {
		return reject("", "data: JSON invalide ("+err.Error()+")")
	}
	env, err := validateApply(record)
	if err != nil {
		if errors.As(err, &errNotApplied{}) {
			// LWW guard rejected the write: return the server's current state.
			current, fetchErr := fetchCurrent(env.ID)
			if fetchErr != nil {
				d.Log.Error("conflict fetch failed", "entity_type", ch.EntityType, "id", env.ID, "err", fetchErr)
				return reject(env.ID, "conflit illisible côté serveur")
			}
			payload, mErr := json.Marshal(current)
			if mErr != nil {
				return reject(env.ID, "conflit non sérialisable")
			}
			return nil, &conflictRef{
				EntityType: ch.EntityType,
				EntityID:   env.ID,
				Reason:     "superseded",
				Current:    payload,
			}, nil
		}
		return reject(env.ID, err.Error())
	}

	seq, err := d.recordChange(ctx, accountID, ch.EntityType, env)
	if err != nil {
		d.Log.Error("change log append failed", "entity_type", ch.EntityType, "id", env.ID, "err", err)
		return reject(env.ID, "échec de l'enregistrement serveur")
	}
	return &appliedRef{EntityType: ch.EntityType, EntityID: env.ID, Seq: seq}, nil, nil
}

// errNotApplied marks an upsert that lost the last-write-wins guard.
type errNotApplied struct{}

func (errNotApplied) Error() string { return "not applied" }

// recordChange appends the applied record to the change log. The payload is
// the canonical JSON of the stored domain record (re-read from the entity
// table would also be valid; the validated input is canonical by contract).
func (d *Deps) recordChange(ctx context.Context, accountID, entityType string, env domain.Envelope) (int64, error) {
	payload, err := d.currentPayload(ctx, accountID, entityType, env.ID)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	deleted := int64(0)
	if env.IsDeleted() {
		deleted = 1
	}
	return d.Q.RecordChange(ctx, dbgen.RecordChangeParams{
		AccountID:  accountID,
		EntityType: entityType,
		EntityID:   env.ID,
		Deleted:    deleted,
		Payload:    ns(payload),
		UpdatedAt:  env.UpdatedAt,
		RecordedAt: now,
	})
}

// currentPayload re-reads the stored row so the change log mirrors the
// server's actual state (the source of truth), not the client's submission.
func (d *Deps) currentPayload(ctx context.Context, accountID, entityType, id string) (*string, error) {
	var v any
	var err error
	switch entityType {
	case domain.TypeCycle:
		var row dbgen.Cycle
		row, err = d.Q.GetCycleByID(ctx, dbgen.GetCycleByIDParams{ID: id, AccountID: accountID})
		v = rowToCycle(row)
	case domain.TypeBleedingObservation:
		var row dbgen.BleedingObservation
		row, err = d.Q.GetBleedingObservationByID(ctx, dbgen.GetBleedingObservationByIDParams{ID: id, AccountID: accountID})
		v = rowToBleeding(row)
	case domain.TypeDailyEntry:
		var row dbgen.DailyEntry
		row, err = d.Q.GetDailyEntryByID(ctx, dbgen.GetDailyEntryByIDParams{ID: id, AccountID: accountID})
		v = rowToDailyEntry(row)
	case domain.TypeSymptomDefinition:
		var row dbgen.SymptomDefinition
		row, err = d.Q.GetSymptomDefinitionByID(ctx, dbgen.GetSymptomDefinitionByIDParams{ID: id, AccountID: accountID})
		v = rowToSymptomDef(row)
	case domain.TypeSymptomLog:
		var row dbgen.SymptomLog
		row, err = d.Q.GetSymptomLogByID(ctx, dbgen.GetSymptomLogByIDParams{ID: id, AccountID: accountID})
		v = rowToSymptomLog(row)
	case domain.TypeBiomarkerObservation:
		var row dbgen.BiomarkerObservation
		row, err = d.Q.GetBiomarkerObservationByID(ctx, dbgen.GetBiomarkerObservationByIDParams{ID: id, AccountID: accountID})
		v = rowToBiomarker(row)
	case domain.TypeMedicationLog:
		var row dbgen.MedicationLog
		row, err = d.Q.GetMedicationLogByID(ctx, dbgen.GetMedicationLogByIDParams{ID: id, AccountID: accountID})
		v = rowToMedication(row)
	default:
		return nil, fmt.Errorf("unknown entity type %q", entityType)
	}
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

// Per-type upsert wrappers translating "0 rows affected" into errNotApplied.

func (d *Deps) upsertCycle(ctx context.Context, accountID string, c domain.Cycle) error {
	n, err := d.Q.UpsertCycle(ctx, cycleParams(accountID, c))
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotApplied{}
	}
	return nil
}

func (d *Deps) upsertBleeding(ctx context.Context, accountID string, b domain.BleedingObservation) error {
	n, err := d.Q.UpsertBleedingObservation(ctx, bleedingParams(accountID, b))
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotApplied{}
	}
	return nil
}

func (d *Deps) upsertDailyEntry(ctx context.Context, accountID string, e domain.DailyEntry) error {
	n, err := d.Q.UpsertDailyEntry(ctx, dailyEntryParams(accountID, e))
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotApplied{}
	}
	return nil
}

func (d *Deps) upsertSymptomDef(ctx context.Context, accountID string, s domain.SymptomDefinition) error {
	n, err := d.Q.UpsertSymptomDefinition(ctx, symptomDefParams(accountID, s))
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotApplied{}
	}
	return nil
}

func (d *Deps) upsertSymptomLog(ctx context.Context, accountID string, s domain.SymptomLog) error {
	n, err := d.Q.UpsertSymptomLog(ctx, symptomLogParams(accountID, s))
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotApplied{}
	}
	return nil
}

func (d *Deps) upsertBiomarker(ctx context.Context, accountID string, b domain.BiomarkerObservation) error {
	n, err := d.Q.UpsertBiomarkerObservation(ctx, biomarkerParams(accountID, b))
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotApplied{}
	}
	return nil
}

func (d *Deps) upsertMedication(ctx context.Context, accountID string, m domain.MedicationLog) error {
	n, err := d.Q.UpsertMedicationLog(ctx, medicationParams(accountID, m))
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotApplied{}
	}
	return nil
}

// Pull --------------------------------------------------------------------

type pullChange struct {
	Seq        int64           `json:"seq"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Deleted    bool            `json:"deleted"`
	UpdatedAt  string          `json:"updated_at"`
	Data       json.RawMessage `json:"data"`
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
			Deleted:    row.Deleted == 1,
			UpdatedAt:  row.UpdatedAt,
			Data:       nil,
		}
		if row.Payload.Valid && !pc.Deleted {
			pc.Data = json.RawMessage(row.Payload.String)
		}
		resp.Changes = append(resp.Changes, pc)
		resp.Cursor = row.Seq
	}
	resp.HasMore = int64(len(rows)) == limit
	writeJSON(w, http.StatusOK, resp)
}
