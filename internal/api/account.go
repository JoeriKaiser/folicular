package api

import (
	"net/http"
	"time"

	"folicular/internal/api/problem"
	"folicular/internal/auth"
	"folicular/internal/db/dbgen"
)

const (
	exportDateMin = "0001-01-01"
	exportDateMax = "9999-12-31"
)

type meResponse struct {
	Account  accountInfo     `json:"account"`
	Device   deviceShort     `json:"device"`
	Settings settingsPayload `json:"settings"`
}

type accountInfo struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type deviceShort struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// settingsPayload carries the sealed settings blob.
//
// Life stage and tracking focuses (pms, pmdd, endometriosis, pcos) are Art. 9
// health data, so they are encrypted client-side like every other record and
// this server stores an opaque blob. It no longer validates them: it cannot
// read them, and the client is the only party that can.
type settingsPayload struct {
	Settings  []byte `json:"settings"`
	UpdatedAt string `json:"updated_at"`
}

// HandleMe returns the account, current device, and server-authoritative
// settings.
func (d *Deps) HandleMe(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	deviceID, _ := auth.DeviceID(r.Context())
	deviceName, _ := auth.DeviceName(r.Context())

	account, err := d.Q.GetAccountByID(r.Context(), accountID)
	if err != nil {
		d.Log.Error("account lookup failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	settings, err := d.Q.GetSettings(r.Context(), accountID)
	if err != nil {
		d.Log.Error("settings lookup failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	writeJSON(w, http.StatusOK, meResponse{
		Account:  accountInfo{ID: account.ID, Status: account.Status, CreatedAt: account.CreatedAt},
		Device:   deviceShort{ID: deviceID, Name: deviceName},
		Settings: settingsToPayload(settings),
	})
}

func settingsToPayload(s dbgen.AccountSetting) settingsPayload {
	return settingsPayload{
		Settings:  s.SettingsCiphertext,
		UpdatedAt: s.UpdatedAt,
	}
}

type patchSettingsRequest struct {
	Settings []byte `json:"settings"`
}

// HandlePatchMe replaces the sealed settings blob.
//
// There is no server-side merge any more: merging requires reading the fields,
// which this server cannot do. The client decrypts, merges, reseals, and sends
// the whole blob.
func (d *Deps) HandlePatchMe(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())

	var req patchSettingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Settings) == 0 {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"settings: requis"))
		return
	}
	if len(req.Settings) > maxCiphertextBytes {
		problem.Write(w, r, problem.Status(http.StatusUnprocessableEntity, "Validation échouée",
			"settings: charge utile trop volumineuse"))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := d.Q.UpdateSettings(r.Context(), dbgen.UpdateSettingsParams{
		SettingsCiphertext: req.Settings,
		UpdatedAt:          now,
		AccountID:          accountID,
	}); err != nil {
		d.Log.Error("settings update failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}


	// Return the full /v1/me body for client convenience.
	d.HandleMe(w, r)
}

// HandleDeleteMe permanently deletes the account and all associated data.
// The schema uses ON DELETE CASCADE on every table referencing accounts,
// so a single DELETE FROM accounts removes everything (GDPR Article 17).
func (d *Deps) HandleDeleteMe(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	if err := d.Q.DeleteAccount(r.Context(), accountID); err != nil {
		d.Log.Error("account deletion failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}
	d.Log.Info("account deleted", "account_id", accountID)
	w.WriteHeader(http.StatusNoContent)
}

// HandleExport returns everything this server holds for the account (GDPR
// Article 20, data portability).
//
// Under end-to-end encryption that means sealed records: the server cannot
// produce a human-readable export because it cannot decrypt. A readable export
// is the client's responsibility, and the client is the only party able to
// produce one. This endpoint still matters - it guarantees the user can
// retrieve the server-side copy in full, and a client holding the account code
// can decrypt it offline.
func (d *Deps) HandleExport(w http.ResponseWriter, r *http.Request) {
	accountID, _ := auth.AccountID(r.Context())
	ctx := r.Context()

	account, err := d.Q.GetAccountByID(ctx, accountID)
	if err != nil {
		d.Log.Error("export: account failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	records, err := d.Q.ListRecordsForAccount(ctx, accountID)
	if err != nil {
		d.Log.Error("export: records failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	settings, err := d.Q.GetSettings(ctx, accountID)
	if err != nil && !isNotFound(err) {
		d.Log.Error("export: settings failed", "err", err)
		problem.Write(w, r, problem.Internal())
		return
	}

	out := exportDocument{
		Format:      "folicular.export.v2.sealed",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Note: "Les enregistrements sont chiffrés de bout en bout. Ce serveur ne " +
			"peut pas les déchiffrer : utilisez votre code de compte dans " +
			"l'application pour obtenir une version lisible.",
		Account:  accountInfo{ID: account.ID, Status: account.Status, CreatedAt: account.CreatedAt},
		Settings: settingsToPayload(settings),
		Records:  make([]exportRecord, 0, len(records)),
	}
	for _, rec := range records {
		out.Records = append(out.Records, exportRecord{
			EntityID:   rec.EntityID,
			EntityType: rec.EntityType,
			ClientRev:  rec.ClientRev,
			Deleted:    rec.Deleted == 1,
			UpdatedAt:  rec.UpdatedAt,
			Ciphertext: rec.Ciphertext,
		})
	}

	writeJSON(w, http.StatusOK, out)
}

type exportDocument struct {
	Format      string          `json:"format"`
	GeneratedAt string          `json:"generated_at"`
	Note        string          `json:"note"`
	Account     accountInfo     `json:"account"`
	Settings    settingsPayload `json:"settings"`
	Records     []exportRecord  `json:"records"`
}

type exportRecord struct {
	EntityID   string `json:"entity_id"`
	EntityType string `json:"entity_type"`
	ClientRev  string `json:"client_rev"`
	Deleted    bool   `json:"deleted"`
	UpdatedAt  string `json:"updated_at"`
	Ciphertext []byte `json:"ciphertext"`
}
