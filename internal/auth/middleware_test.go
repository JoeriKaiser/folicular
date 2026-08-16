package auth

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"folicular/internal/db"
	"folicular/internal/db/dbgen"
)

func TestBearerToken(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"empty", "", ""},
		{"basic auth", "Basic dXNlcjpwYXNz", ""},
		{"bearer lowercase", "bearer my-token", "my-token"},
		{"bearer mixed case", "BeArEr my-token", "my-token"},
		{"bearer with extra spaces", "Bearer   my-token   ", "my-token"},
		{"bearer no token", "Bearer ", ""},
		{"bearer only", "Bearer", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			got := bearerToken(req)
			if got != tc.want {
				t.Errorf("bearerToken() = %q, want %q", got, tc.want)
			}
		})
	}
}

func setupTestDB(t *testing.T) (*sql.DB, *dbgen.Queries) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "auth_test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return sqlDB, dbgen.New(sqlDB)
}

func TestMiddleware(t *testing.T) {
	_, q := setupTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw := Middleware(q, log)

	now := time.Now().UTC().Format(time.RFC3339)

	// Create an active account and valid device
	accountID := "019832e0-0000-7000-8000-000000000001"
	if err := q.InsertAccount(t.Context(), dbgen.InsertAccountParams{
		ID: accountID, CodeHash: HashCode("CODE1"), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("insert account: %v", err)
	}

	validToken := "ltok_valid_token_123"
	deviceID := "019832e0-0000-7000-8000-000000000002"
	deviceName := "Pixel 9"
	if err := q.InsertDevice(t.Context(), dbgen.InsertDeviceParams{
		ID: deviceID, AccountID: accountID, Name: deviceName,
		TokenHash: HashToken(validToken), CreatedAt: now,
	}); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	// Create a revoked device
	revokedToken := "ltok_revoked_token_456"
	revokedDeviceID := "019832e0-0000-7000-8000-000000000003"
	if err := q.InsertDevice(t.Context(), dbgen.InsertDeviceParams{
		ID: revokedDeviceID, AccountID: accountID, Name: "Revoked Phone",
		TokenHash: HashToken(revokedToken), CreatedAt: now,
	}); err != nil {
		t.Fatalf("insert revoked device: %v", err)
	}
	if _, err := q.RevokeDevice(t.Context(), dbgen.RevokeDeviceParams{
		RevokedAt: sql.NullString{String: now, Valid: true},
		ID:        revokedDeviceID,
		AccountID: accountID,
	}); err != nil {
		t.Fatalf("revoke device: %v", err)
	}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accID, ok := AccountID(r.Context())
		if !ok || accID != accountID {
			t.Errorf("expected account ID %q in context, got %q", accountID, accID)
		}
		devID, ok := DeviceID(r.Context())
		if !ok || devID != deviceID {
			t.Errorf("expected device ID %q in context, got %q", deviceID, devID)
		}
		devName, ok := DeviceName(r.Context())
		if !ok || devName != deviceName {
			t.Errorf("expected device name %q in context, got %q", deviceName, devName)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(nextHandler)

	t.Run("missing authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("unknown token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set("Authorization", "Bearer ltok_nonexistent_token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("revoked token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+revokedToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+validToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
}
