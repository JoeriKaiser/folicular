package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"folicular/internal/api/problem"
	"folicular/internal/auth"
	"folicular/internal/db/dbgen"
	"folicular/internal/domain"
)

// builtinSymptoms seeds the canonical symptom catalog for a new account.
// Labels are French-first. The client must adopt these definitions (matched
// by key) rather than creating its own built-in rows, so all devices
// converge on one catalog.
var builtinSymptoms = []domain.SymptomDefinition{
	{Key: "cramps", Label: "Crampes", Category: "pain"},
	{Key: "headache", Label: "Maux de tête", Category: "pain"},
	{Key: "back_pain", Label: "Douleur lombaire", Category: "pain"},
	{Key: "mood_changes", Label: "Changements d'humeur", Category: "mood"},
	{Key: "anxiety", Label: "Anxiété", Category: "mood"},
	{Key: "fatigue", Label: "Fatigue", Category: "energy"},
	{Key: "bloating", Label: "Ballonnements", Category: "physical"},
	{Key: "acne", Label: "Acné", Category: "physical"},
	{Key: "breast_tenderness", Label: "Seins sensibles", Category: "physical"},
	{Key: "cervical_fluid", Label: "Glaire cervicale", Category: "cervical_fluid"},
}

type registerRequest struct {
	DeviceName string `json:"device_name"`
	InviteCode string `json:"invite_code"`
}

type accountCodeResponse struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

type deviceTokenResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

type registerResponse struct {
	Account accountCodeResponse `json:"account"`
	Device  deviceTokenResponse `json:"device"`
	Warning string              `json:"warning"`
}

// HandleRegister creates an anonymous account and its first device. The
// account code is shown exactly once; only its hash is stored.
func (d *Deps) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if d.registrationGated() && !d.validInviteCode(req.InviteCode) {
		problem.Write(w, r, problem.Status(http.StatusUnauthorized, "Inscription refusée",
			"Un code d'invitation valide est requis pour créer un compte."))
		return
	}
	req.DeviceName = strings.TrimSpace(req.DeviceName)
	if len(req.DeviceName) > 120 {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée", "device_name: dépasse la longueur maximale"))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	accountID := uuid.NewString()
	displayCode, codeHash, err := auth.GenerateAccountCode()
	if err != nil {
		d.Log.Error("account code generation failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	deviceID := uuid.NewString()
	token, tokenHash, err := auth.GenerateDeviceToken()
	if err != nil {
		d.Log.Error("device token generation failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	err = d.txFunc(r, func(q *dbgen.Queries) error {
		if err := q.InsertAccount(r.Context(), dbgen.InsertAccountParams{
			ID: accountID, CodeHash: codeHash, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		if err := q.InsertDefaultSettings(r.Context(), dbgen.InsertDefaultSettingsParams{
			AccountID: accountID, UpdatedAt: now,
		}); err != nil {
			return err
		}
		if err := seedBuiltinSymptoms(r.Context(), q, accountID, now); err != nil {
			return err
		}
		return q.InsertDevice(r.Context(), dbgen.InsertDeviceParams{
			ID: deviceID, AccountID: accountID, Name: req.DeviceName,
			TokenHash: tokenHash, CreatedAt: now,
		})
	})
	if err != nil {
		d.Log.Error("register failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	writeJSON(w, http.StatusCreated, registerResponse{
		Account: accountCodeResponse{ID: accountID, Code: displayCode},
		Device:  deviceTokenResponse{ID: deviceID, Name: req.DeviceName, Token: token},
		Warning: "Le code de compte est affiché une seule fois. Conservez-le en lieu sûr : il permet seul de retrouver votre compte et d'ajouter des périphériques.",
	})
}

// seedBuiltinSymptoms inserts the canonical catalog and records each row in
// the change log so every device converges on the same definitions.
func seedBuiltinSymptoms(ctx context.Context, q *dbgen.Queries, accountID, now string) error {
	for _, s := range builtinSymptoms {
		s.ID = uuid.NewString()
		s.ClientRev = uuid.NewString()
		s.CreatedAt = now
		s.UpdatedAt = now
		s.Builtin = true
		s.Active = true
		if err := s.Validate(); err != nil {
			return err
		}
		if _, err := q.UpsertSymptomDefinition(ctx, symptomDefParams(accountID, s)); err != nil {
			return err
		}
		payload, err := json.Marshal(s)
		if err != nil {
			return err
		}
		if _, err := q.RecordChange(ctx, dbgen.RecordChangeParams{
			AccountID:  accountID,
			EntityType: domain.TypeSymptomDefinition,
			EntityID:   s.ID,
			Deleted:    0,
			Payload:    ns(stringPtr(string(payload))),
			UpdatedAt:  s.UpdatedAt,
			RecordedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func stringPtr(s string) *string { return &s }

type addDeviceRequest struct {
	Code       string `json:"code"`
	DeviceName string `json:"device_name"`
}

type addDeviceResponse struct {
	Device deviceTokenResponse `json:"device"`
}

// HandleAddDevice registers an additional device using the account code.
// Rate limited upstream; failures are generic to prevent enumeration.
func (d *Deps) HandleAddDevice(w http.ResponseWriter, r *http.Request) {
	var req addDeviceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	normalized := auth.NormalizeCode(req.Code)
	account, err := d.Q.GetAccountByCodeHash(r.Context(), auth.HashCode(normalized))
	if err != nil || account.Status != "active" {
		problem.Write(w, r, problem.Status(http.StatusUnauthorized, "Code invalide", "Le code de compte est invalide."))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	deviceID := uuid.NewString()
	token, tokenHash, err := auth.GenerateDeviceToken()
	if err != nil {
		d.Log.Error("device token generation failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	name := strings.TrimSpace(req.DeviceName)
	if len(name) > 120 {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée", "device_name: dépasse la longueur maximale"))
		return
	}
	if err := d.Q.InsertDevice(r.Context(), dbgen.InsertDeviceParams{
		ID: deviceID, AccountID: account.ID, Name: name, TokenHash: tokenHash, CreatedAt: now,
	}); err != nil {
		d.Log.Error("device insert failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	writeJSON(w, http.StatusCreated, addDeviceResponse{
		Device: deviceTokenResponse{ID: deviceID, Name: name, Token: token},
	})
}

type deviceView struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"created_at"`
	LastSeenAt *string `json:"last_seen_at"`
	Revoked    bool    `json:"revoked"`
	Current    bool    `json:"current"`
}

// HandleListDevices lists the account's devices.
func (d *Deps) HandleListDevices(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	currentDeviceID, _ := auth.DeviceID(r.Context())
	rows, err := d.Q.ListDevicesByAccount(r.Context(), accountID)
	if err != nil {
		d.Log.Error("device list failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	out := make([]deviceView, 0, len(rows))
	for _, row := range rows {
		out = append(out, deviceView{
			ID:         row.ID,
			Name:       row.Name,
			CreatedAt:  row.CreatedAt,
			LastSeenAt: sp(row.LastSeenAt),
			Revoked:    row.RevokedAt.Valid,
			Current:    row.ID == currentDeviceID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

// HandleRevokeDevice revokes a device token belonging to the account.
func (d *Deps) HandleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	deviceID := strings.TrimSpace(chi.URLParam(r, "deviceID"))
	if deviceID == "" {
		problem.Write(w, r, problem.Status(http.StatusBadRequest, "Requête invalide", "Identifiant de périphérique manquant."))
		return
	}
	rowsAffected, err := d.Q.RevokeDevice(r.Context(), dbgen.RevokeDeviceParams{
		RevokedAt: ns(stringPtr(time.Now().UTC().Format(time.RFC3339))),
		ID:        deviceID,
		AccountID: accountID,
	})
	if err != nil {
		d.Log.Error("device revoke failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	if rowsAffected == 0 {
		problem.Write(w, r, problem.Status(http.StatusNotFound, "Introuvable", "Périphérique inconnu pour ce compte."))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
