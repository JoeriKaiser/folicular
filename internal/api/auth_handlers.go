package api

import (
	"errors"
	"fmt"
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
	AuthKey    string `json:"auth_key"`
	AuthHash   string `json:"auth_hash"` // deprecated in favor of auth_key; rejected with 422
	Code       string `json:"code"`
}

type accountCodeResponse struct {
	ID          string `json:"id"`
	AuthVersion int64  `json:"auth_version"`
	Code        string `json:"code,omitempty"`
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

type txProblemError struct {
	status int
	title  string
	detail string
}

func (e txProblemError) Error() string { return e.detail }

func isUniqueCodeHashErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: accounts.code_hash")
}

// HandleRegister creates a pseudonymous account and its first device.
//
// When auth_key is supplied, the client generated the root recovery secret
// locally and derived K_auth via HKDF. The server hashes K_auth with a
// domain-separated SHA-256 verifier before storing it (auth_version = 2) and
// never observes the recovery code or encryption key.
//
// Otherwise, legacy v1 registration generates or accepts a 100-bit Crockford
// code (auth_version = 1).
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
	if strings.TrimSpace(req.AuthHash) != "" {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"auth_hash n'est plus pris en charge ; fournissez auth_key"))
		return
	}
	if strings.TrimSpace(req.AuthKey) != "" && strings.TrimSpace(req.Code) != "" {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"auth_key et code ne peuvent pas être fournis simultanément"))
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
		authVersion int64
		displayCode string
	)
	if strings.TrimSpace(req.AuthKey) != "" {
		parsed, err := auth.ParseAuthKey(req.AuthKey)
		if err != nil {
			problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
				"auth_key: format invalide (attendu 32 octets en hex ou base64)"))
			return
		}
		codeHash = auth.HashAuthKey(parsed)
		authVersion = auth.AuthVersionV2
	} else if strings.TrimSpace(req.Code) != "" {
		norm := auth.NormalizeCode(req.Code)
		if len(norm) != 20 {
			problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée", "code: format de code de compte invalide"))
			return
		}
		codeHash = auth.HashCode(norm)
		authVersion = auth.AuthVersionLegacy
		displayCode = req.Code
	} else {
		var err error
		displayCode, codeHash, err = auth.GenerateAccountCode()
		if err != nil {
			d.Log.Error("account code generation failed", "err", err)
			problem.Write(w, r, problem.Internal())
			return
		}
		authVersion = auth.AuthVersionLegacy
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
			ID: accountID, CodeHash: codeHash, AuthVersion: authVersion, CreatedAt: now, UpdatedAt: now,
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
		if isUniqueCodeHashErr(err) {
			problem.Write(w, r, problem.Status(http.StatusConflict, "Conflit",
				"Cette clé d'authentification est déjà enregistrée."))
			return
		}
		d.Log.Error("register failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	warning := "Le code de compte est affiché une seule fois. Conservez-le en lieu sûr : il permet seul de retrouver votre compte et d'ajouter des périphériques."
	if authVersion == auth.AuthVersionV2 {
		warning = "Conservez votre code de récupération en lieu sûr : ce serveur ne l'a jamais reçu et ne peut pas le réinitialiser."
	}
	writeJSON(w, http.StatusCreated, registerResponse{
		Account: accountCodeResponse{ID: accountID, AuthVersion: authVersion, Code: displayCode},
		Device:  deviceTokenResponse{ID: deviceID, Name: req.DeviceName, Token: token},
		Warning: warning,
	})
}

type addDeviceRequest struct {
	Code       string `json:"code"`
	AuthKey    string `json:"auth_key"`
	AuthHash   string `json:"auth_hash"` // deprecated; rejected with generic 401
	DeviceName string `json:"device_name"`
}

type addDeviceResponse struct {
	// The account id is returned alongside the device because the client needs
	// it to derive its encryption keys (v1 HKDF salt, v2 HKDF-Expand info).
	// It is not a secret, and the caller has just proved possession of the
	// account credential to reach this point.
	AccountID   string              `json:"account_id"`
	AuthVersion int64               `json:"auth_version"`
	Device      deviceTokenResponse `json:"device"`
}

// HandleAddDevice registers an additional device using either a v2 auth_key
// or a legacy v1 account code. Rate limited upstream; failures are generic to
// prevent enumeration.
func (d *Deps) HandleAddDevice(w http.ResponseWriter, r *http.Request) {
	var req addDeviceRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	invalidCreds := func() {
		problem.Write(w, r, problem.Status(http.StatusUnauthorized, "Code invalide", "Le code de compte est invalide."))
	}

	hasAuthKey := strings.TrimSpace(req.AuthKey) != ""
	hasCode := strings.TrimSpace(req.Code) != ""
	if strings.TrimSpace(req.AuthHash) != "" || hasAuthKey == hasCode {
		invalidCreds()
		return
	}

	var (
		verifier        []byte
		expectedVersion int64
	)
	if hasAuthKey {
		parsed, err := auth.ParseAuthKey(req.AuthKey)
		if err != nil {
			invalidCreds()
			return
		}
		verifier = auth.HashAuthKey(parsed)
		expectedVersion = auth.AuthVersionV2
	} else {
		normalized := auth.NormalizeCode(req.Code)
		verifier = auth.HashCode(normalized)
		expectedVersion = auth.AuthVersionLegacy
	}

	account, err := d.Q.GetAccountByCodeHash(r.Context(), verifier)
	if err != nil || account.Status != "active" || account.AuthVersion != expectedVersion {
		invalidCreds()
		return
	}
	if !auth.VerifierMatches(verifier, account.CodeHash) {
		invalidCreds()
		return
	}

	name := strings.TrimSpace(req.DeviceName)
	if len(name) > 120 {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée", "device_name: dépasse la longueur maximale"))
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
	if err := d.Q.InsertDevice(r.Context(), dbgen.InsertDeviceParams{
		ID: deviceID, AccountID: account.ID, Name: name, TokenHash: tokenHash, CreatedAt: now,
	}); err != nil {
		d.Log.Error("device insert failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	writeJSON(w, http.StatusCreated, addDeviceResponse{
		AccountID:   account.ID,
		AuthVersion: account.AuthVersion,
		Device:      deviceTokenResponse{ID: deviceID, Name: name, Token: token},
	})
}

type upgradeRecordInput struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	ClientRev  string `json:"client_rev"`
	UpdatedAt  string `json:"updated_at"`
	Deleted    bool   `json:"deleted"`
	Ciphertext []byte `json:"ciphertext"`
}

type upgradeRequest struct {
	AuthKey            string               `json:"auth_key"`
	ExpectedCursor     int64                `json:"expected_cursor"`
	RevokeOtherDevices *bool                `json:"revoke_other_devices"`
	Settings           []byte               `json:"settings"`
	Records            []upgradeRecordInput `json:"records"`
}

type upgradeResponse struct {
	AccountID       string `json:"account_id"`
	AuthVersion     int64  `json:"auth_version"`
	Cursor          int64  `json:"cursor"`
	RecordsMigrated int    `json:"records_migrated"`
	RevokedDevices  int64  `json:"revoked_devices"`
}

// HandleUpgrade atomically migrates an authenticated v1 account in place to
// operator-blind v2 encryption:
//   - verifies expected_cursor (optimistic concurrency against concurrent v1 pushes)
//   - verifies every live server record is re-sealed as a valid 0x02 envelope
//   - rotates accounts.code_hash to SHA-256(domain || auth_key) and auth_version = 2
//   - replaces records and purges all legacy 0x01 rows from sync_changes
//   - preserves account_id, Duo links, grants, payloads, and support requests
//   - revokes other devices by default while keeping the calling device token active
//
// Retrying with the same auth_key after an already-committed upgrade returns
// 200 OK idempotently so a dropped response never leaves the client stranded.
func (d *Deps) HandleUpgrade(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	currentDeviceID, _ := auth.DeviceID(r.Context())

	var req upgradeRequest
	if !decodeJSONWithLimit(w, r, &req, maxUpgradeBodyBytes) {
		return
	}

	parsedKey, err := auth.ParseAuthKey(req.AuthKey)
	if err != nil {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"auth_key: format invalide (attendu 32 octets en hex ou base64)"))
		return
	}
	newVerifier := auth.HashAuthKey(parsedKey)

	if req.ExpectedCursor < 0 {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"expected_cursor: entier positif ou nul attendu"))
		return
	}

	if len(req.Settings) > 0 {
		if err := validateSealedEnvelope(req.Settings, auth.AuthVersionV2); err != nil {
			problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
				"settings: "+err.Error()))
			return
		}
	}

	incomingByID := make(map[string]upgradeRecordInput, len(req.Records))
	for i, rec := range req.Records {
		if !domain.IsEntityType(rec.EntityType) {
			problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
				fmt.Sprintf("records[%d]: entity_type: type inconnu %q", i, rec.EntityType)))
			return
		}
		if !domain.IsUUID(rec.EntityID) {
			problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
				fmt.Sprintf("records[%d]: entity_id: UUID invalide", i)))
			return
		}
		if !domain.IsUUID(rec.ClientRev) {
			problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
				fmt.Sprintf("records[%d]: client_rev: UUID invalide", i)))
			return
		}
		if !domain.IsInstant(rec.UpdatedAt) {
			problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
				fmt.Sprintf("records[%d]: updated_at: instant RFC 3339 invalide", i)))
			return
		}
		if _, dup := incomingByID[rec.EntityID]; dup {
			problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
				fmt.Sprintf("records[%d]: entity_id en double %q", i, rec.EntityID)))
			return
		}
		if !rec.Deleted {
			if err := validateSealedEnvelope(rec.Ciphertext, auth.AuthVersionV2); err != nil {
				problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
					fmt.Sprintf("records[%d]: %s", i, err.Error())))
				return
			}
		}
		incomingByID[rec.EntityID] = rec
	}

	revokeOther := true
	if req.RevokeOtherDevices != nil {
		revokeOther = *req.RevokeOtherDevices
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var resp upgradeResponse

	err = d.txFunc(r, func(q *dbgen.Queries) error {
		ctx := r.Context()
		account, err := q.GetAccountByID(ctx, accountID)
		if err != nil {
			return err
		}

		// Idempotent completion if this exact upgrade already committed.
		if account.AuthVersion == auth.AuthVersionV2 {
			if auth.VerifierMatches(newVerifier, account.CodeHash) {
				cur, err := q.LatestCursor(ctx, accountID)
				if err != nil && !isNotFound(err) {
					return err
				}
				resp = upgradeResponse{
					AccountID:       accountID,
					AuthVersion:     auth.AuthVersionV2,
					Cursor:          cur,
					RecordsMigrated: len(req.Records),
					RevokedDevices:  0,
				}
				return nil
			}
			return txProblemError{
				status: http.StatusConflict,
				title:  "Conflit",
				detail: "Ce compte utilise déjà le chiffrement v2 avec une autre clé.",
			}
		}

		latestCursor, err := q.LatestCursor(ctx, accountID)
		if err != nil && !isNotFound(err) {
			return err
		}
		if latestCursor != req.ExpectedCursor {
			return txProblemError{
				status: http.StatusConflict,
				title:  "Conflit de synchronisation",
				detail: "Des modifications plus récentes existent sur le serveur ; synchronisez avant la mise à niveau.",
			}
		}

		// Completeness guard: every non-deleted server record must be re-sealed.
		existingRecords, err := q.ListRecordsForAccount(ctx, accountID)
		if err != nil {
			return err
		}
		for _, cur := range existingRecords {
			if cur.Deleted == 1 {
				continue
			}
			inc, ok := incomingByID[cur.EntityID]
			if !ok || inc.Deleted || inc.EntityType != cur.EntityType {
				return txProblemError{
					status: http.StatusUnprocessableEntity,
					title:  "Mise à niveau incomplète",
					detail: fmt.Sprintf("L'enregistrement %s (%s) doit être rechiffré en v2.", cur.EntityID, cur.EntityType),
				}
			}
		}

		existingSettings, err := q.GetSettings(ctx, accountID)
		if err != nil && !isNotFound(err) {
			return err
		}
		if len(existingSettings.SettingsCiphertext) > 0 && len(req.Settings) == 0 {
			return txProblemError{
				status: http.StatusUnprocessableEntity,
				title:  "Mise à niveau incomplète",
				detail: "Les paramètres chiffrés existants doivent être rechiffrés en v2.",
			}
		}

		if err := q.UpdateAccountAuth(ctx, dbgen.UpdateAccountAuthParams{
			CodeHash:    newVerifier,
			AuthVersion: auth.AuthVersionV2,
			UpdatedAt:   now,
			ID:          accountID,
		}); err != nil {
			return err
		}

		if err := q.DeleteRecordsForAccount(ctx, accountID); err != nil {
			return err
		}
		if err := q.DeleteSyncChangesForAccount(ctx, accountID); err != nil {
			return err
		}

		var newCursor int64
		for _, rec := range req.Records {
			deleted := int64(0)
			var ciphertext []byte
			if rec.Deleted {
				deleted = 1
			} else {
				ciphertext = rec.Ciphertext
			}
			if err := q.InsertRecord(ctx, dbgen.InsertRecordParams{
				AccountID:  accountID,
				EntityID:   rec.EntityID,
				EntityType: rec.EntityType,
				ClientRev:  rec.ClientRev,
				Ciphertext: ciphertext,
				Deleted:    deleted,
				UpdatedAt:  rec.UpdatedAt,
				RecordedAt: now,
			}); err != nil {
				return err
			}
			seq, err := q.RecordChange(ctx, dbgen.RecordChangeParams{
				AccountID:  accountID,
				EntityType: rec.EntityType,
				EntityID:   rec.EntityID,
				ClientRev:  rec.ClientRev,
				Deleted:    deleted,
				Ciphertext: ciphertext,
				UpdatedAt:  rec.UpdatedAt,
				RecordedAt: now,
			})
			if err != nil {
				return err
			}
			newCursor = seq
		}

		if len(req.Settings) > 0 {
			if err := q.UpdateSettings(ctx, dbgen.UpdateSettingsParams{
				SettingsCiphertext: req.Settings,
				UpdatedAt:          now,
				AccountID:          accountID,
			}); err != nil {
				return err
			}
		}

		var revoked int64
		if revokeOther {
			revoked, err = q.RevokeOtherDevices(ctx, dbgen.RevokeOtherDevicesParams{
				RevokedAt: ns(stringPtr(now)),
				AccountID: accountID,
				ID:        currentDeviceID,
			})
			if err != nil {
				return err
			}
		}

		resp = upgradeResponse{
			AccountID:       accountID,
			AuthVersion:     auth.AuthVersionV2,
			Cursor:          newCursor,
			RecordsMigrated: len(req.Records),
			RevokedDevices:  revoked,
		}
		return nil
	})
	if err != nil {
		var tp txProblemError
		if errors.As(err, &tp) {
			problem.Write(w, r, problem.Status(tp.status, tp.title, tp.detail))
			return
		}
		if isUniqueCodeHashErr(err) {
			problem.Write(w, r, problem.Status(http.StatusConflict, "Conflit",
				"Cette clé d'authentification est déjà utilisée par un autre compte."))
			return
		}
		d.Log.Error("account upgrade failed", "account_id", accountID, "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	d.Log.Info("account upgraded to v2",
		"account_id", accountID,
		"records_migrated", resp.RecordsMigrated,
		"revoked_devices", resp.RevokedDevices,
	)
	writeJSON(w, http.StatusOK, resp)
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
