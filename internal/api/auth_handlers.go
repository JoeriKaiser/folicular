package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"folicular/internal/api/problem"
	"folicular/internal/auth"
	"folicular/internal/db/dbgen"
)

// The built-in symptom catalog used to be seeded here. Under end-to-end
// encryption the server cannot create records: it has no key, so anything it
// wrote would be unreadable by the client and would corrupt the change log.
//
// Nothing replaces it server-side, and nothing needs to: the client renders its
// own catalog (Symptom.DEFAULT_SYMPTOMS) and symptom definitions are not part
// of the synced slice today. If they are added later they arrive sealed, like
// any other record, and the client reconciles them (SymptomCatalogAdopter).
// There is no longer a server-side canonical vocabulary, by construction.

type registerRequest struct {
	DeviceName string `json:"device_name"`
	InviteCode string `json:"invite_code"`
	AuthHash   string `json:"auth_hash"`
	Code       string `json:"code"`
}

type accountCodeResponse struct {
	ID   string `json:"id"`
	Code string `json:"code,omitempty"`
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
	var (
		codeHash    []byte
		displayCode string
	)
	if strings.TrimSpace(req.AuthHash) != "" {
		parsed, err := auth.ParseAuthHash(req.AuthHash)
		if err != nil {
			problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée", "auth_hash: format de hachage invalide (attendu 32 octets en hex ou base64)"))
			return
		}
		codeHash = parsed
	} else if strings.TrimSpace(req.Code) != "" {
		norm := auth.NormalizeCode(req.Code)
		if len(norm) != 20 {
			problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée", "code: format de code de compte invalide"))
			return
		}
		codeHash = auth.HashCode(norm)
		displayCode = req.Code
	} else {
		var err error
		displayCode, codeHash, err = auth.GenerateAccountCode()
		if err != nil {
			d.Log.Error("account code generation failed", "err", err)
			problem.Write(w, r, problem.Internal())
			return
		}
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

	warning := "Le code de compte est affiché une seule fois. Conservez-le en lieu sûr : il permet seul de retrouver votre compte et d'ajouter des périphériques."
	if displayCode == "" {
		warning = "Conservez vos clés de récupération en lieu sûr : elles permettent seules de retrouver votre compte et d'ajouter des périphériques."
	}
	writeJSON(w, http.StatusCreated, registerResponse{
		Account: accountCodeResponse{ID: accountID, Code: displayCode},
		Device:  deviceTokenResponse{ID: deviceID, Name: req.DeviceName, Token: token},
		Warning: warning,
	})
}



type addDeviceRequest struct {
	Code       string `json:"code"`
	AuthHash   string `json:"auth_hash"`
	DeviceName string `json:"device_name"`
}

type addDeviceResponse struct {
	// The account id is returned alongside the device because the client needs
	// it to derive its encryption keys (it is the HKDF salt, see the client's
	// E2EE_DESIGN.md section 3). It is not a secret, and the caller has just
	// proved possession of the account code to reach this point. Returning it
	// here saves a follow-up /v1/me round trip during recovery.
	AccountID string              `json:"account_id"`
	Device    deviceTokenResponse `json:"device"`
}

// HandleAddDevice registers an additional device using the account code.
// Rate limited upstream; failures are generic to prevent enumeration.
func (d *Deps) HandleAddDevice(w http.ResponseWriter, r *http.Request) {
	var req addDeviceRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	var codeHash []byte
	if strings.TrimSpace(req.AuthHash) != "" {
		parsed, err := auth.ParseAuthHash(req.AuthHash)
		if err != nil {
			problem.Write(w, r, problem.Status(http.StatusUnauthorized, "Code invalide", "Le code de compte est invalide."))
			return
		}
		codeHash = parsed
	} else if strings.TrimSpace(req.Code) != "" {
		normalized := auth.NormalizeCode(req.Code)
		codeHash = auth.HashCode(normalized)
	} else {
		problem.Write(w, r, problem.Status(http.StatusUnauthorized, "Code invalide", "Le code de compte est invalide."))
		return
	}

	account, err := d.Q.GetAccountByCodeHash(r.Context(), codeHash)
	if err != nil || account.Status != "active" {
		problem.Write(w, r, problem.Status(http.StatusUnauthorized, "Code invalide", "Le code de compte est invalide."))
		return
	}
	if !auth.AuthHashMatches(codeHash, account.CodeHash) {
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
		AccountID: account.ID,
		Device:    deviceTokenResponse{ID: deviceID, Name: name, Token: token},
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
