package config

import (
	"os"
	"testing"
)

func TestLoad_PairingBaseURL(t *testing.T) {
	tests := []struct {
		name      string
		envVal    string
		setEnv    bool
		wantURL   string
		isDefault bool
	}{
		{
			name:      "default when unset",
			setEnv:    false,
			wantURL:   DefaultPairingBaseURL,
			isDefault: true,
		},
		{
			name:      "default when empty",
			envVal:    "",
			setEnv:    true,
			wantURL:   DefaultPairingBaseURL,
			isDefault: true,
		},
		{
			name:      "custom url without trailing slash",
			envVal:    "https://duo.example.com",
			setEnv:    true,
			wantURL:   "https://duo.example.com",
			isDefault: false,
		},
		{
			name:      "custom url with trailing slash",
			envVal:    "https://duo.example.com/",
			setEnv:    true,
			wantURL:   "https://duo.example.com",
			isDefault: false,
		},
		{
			name:      "custom url with whitespace",
			envVal:    "  https://duo.example.com/  ",
			setEnv:    true,
			wantURL:   "https://duo.example.com",
			isDefault: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("FOLICULAR_PAIRING_BASE_URL", tt.envVal)
			} else {
				os.Unsetenv("FOLICULAR_PAIRING_BASE_URL")
			}

			cfg := Load()
			if cfg.PairingBaseURL != tt.wantURL {
				t.Errorf("PairingBaseURL = %q, want %q", cfg.PairingBaseURL, tt.wantURL)
			}
			if got := cfg.IsDefaultPairingBaseURL(); got != tt.isDefault {
				t.Errorf("IsDefaultPairingBaseURL() = %v, want %v", got, tt.isDefault)
			}
		})
	}
}
