package config

import (
	"strings"
	"testing"
)

func TestLoadAppTimezone(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example.invalid/learnd")
	t.Setenv("API_TOKEN", "test-token")

	t.Run("defaults to America/New_York", func(t *testing.T) {
		t.Setenv("APP_TIMEZONE", "")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Location.String() != "America/New_York" {
			t.Fatalf("Location = %s, want America/New_York", cfg.Location)
		}
	})

	t.Run("accepts an IANA zone", func(t *testing.T) {
		t.Setenv("APP_TIMEZONE", " Europe/London ")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Location.String() != "Europe/London" {
			t.Fatalf("Location = %s, want Europe/London", cfg.Location)
		}
	})

	t.Run("rejects an unknown zone", func(t *testing.T) {
		t.Setenv("APP_TIMEZONE", "Mars/Olympus_Mons")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "APP_TIMEZONE") {
			t.Fatalf("Load() error = %v, want APP_TIMEZONE error", err)
		}
	})
}
