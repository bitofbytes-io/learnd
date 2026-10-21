package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetricsAddrFileValidation(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example.invalid/learnd")
	t.Setenv("API_TOKEN", "test-token")
	t.Setenv("METRICS_ADDR_FILE", "")
	path := filepath.Join(t.TempDir(), "metrics-addr")
	t.Setenv("METRICS_ADDR", "127.0.0.1:9500")
	t.Setenv("METRICS_ADDR_FILE", path)
	for _, tc := range []struct {
		value, want string
		invalid     bool
	}{
		{" OfF\n", "off", false}, {"[::1]:9501\n", "[::1]:9501", false}, {"bad/path:9464", "", true},
	} {
		if err := os.WriteFile(path, []byte(tc.value), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if tc.invalid {
			if err == nil || !strings.Contains(err.Error(), "METRICS_ADDR") {
				t.Fatalf("invalid file address accepted: %v", err)
			}
		} else if err != nil || cfg.MetricsAddr != tc.want {
			t.Fatalf("file value=%q cfg=%v err=%v", tc.value, cfg, err)
		}
	}
}
