package domain

import (
	"testing"
	"time"
)

func TestEnvelopeValidate(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	validID := "019832e0-6c14-7000-8000-000000000001"
	validRev := "019832e0-6c14-7000-8000-000000000002"

	t.Run("valid envelope", func(t *testing.T) {
		env := Envelope{
			ID:        validID,
			ClientRev: validRev,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := env.validate(); err != nil {
			t.Errorf("expected valid envelope, got error: %v", err)
		}
		if env.IsDeleted() {
			t.Error("expected IsDeleted() = false")
		}
	})

	t.Run("valid tombstone envelope", func(t *testing.T) {
		deletedAt := now
		env := Envelope{
			ID:        validID,
			ClientRev: validRev,
			CreatedAt: now,
			UpdatedAt: now,
			DeletedAt: &deletedAt,
		}
		if err := env.validate(); err != nil {
			t.Errorf("expected valid tombstone, got error: %v", err)
		}
		if !env.IsDeleted() {
			t.Error("expected IsDeleted() = true")
		}
	})

	t.Run("invalid UUIDs", func(t *testing.T) {
		env := Envelope{
			ID:        "not-a-uuid",
			ClientRev: "also-not-a-uuid",
			CreatedAt: now,
			UpdatedAt: now,
		}
		err := env.validate()
		if err == nil {
			t.Fatal("expected error for invalid UUIDs")
		}
	})

	t.Run("invalid timestamp format", func(t *testing.T) {
		env := Envelope{
			ID:        validID,
			ClientRev: validRev,
			CreatedAt: "2026-07-21", // date only, not instant
			UpdatedAt: "invalid-time",
		}
		err := env.validate()
		if err == nil {
			t.Fatal("expected error for non-RFC3339 timestamps")
		}
	})

	t.Run("future timestamp clock skew rejection", func(t *testing.T) {
		future := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)
		env := Envelope{
			ID:        validID,
			ClientRev: validRev,
			CreatedAt: now,
			UpdatedAt: future,
		}
		err := env.validate()
		if err == nil {
			t.Fatal("expected clock skew error for updated_at > 5 minutes in future")
		}
	})
}

func TestIsEntityType(t *testing.T) {
	for _, typ := range EntityTypes {
		if !IsEntityType(typ) {
			t.Errorf("IsEntityType(%q) = false, want true", typ)
		}
	}

	invalidTypes := []string{"", "user", "prediction", "unknown", "cycle_record"}
	for _, typ := range invalidTypes {
		if IsEntityType(typ) {
			t.Errorf("IsEntityType(%q) = true, want false", typ)
		}
	}
}

func TestIsUUID(t *testing.T) {
	if !IsUUID("019832e0-6c14-7000-8000-000000000001") {
		t.Error("IsUUID() = false for valid UUID")
	}
	if IsUUID("12345") {
		t.Error("IsUUID() = true for invalid UUID")
	}
	if IsUUID("") {
		t.Error("IsUUID() = true for empty string")
	}
}

func TestIsInstant(t *testing.T) {
	if !IsInstant("2026-07-21T14:03:00Z") {
		t.Error("IsInstant() = false for valid RFC 3339 instant")
	}
	if IsInstant("2026-07-21") {
		t.Error("IsInstant() = true for date-only string")
	}
	if IsInstant("invalid") {
		t.Error("IsInstant() = true for invalid string")
	}
}
