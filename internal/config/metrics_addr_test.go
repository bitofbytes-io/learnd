package config

import (
	"os"
	"strings"
	"testing"
)

func TestMetricsAddrValidation(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example.invalid/learnd")
	t.Setenv("API_TOKEN", "test-token")
	t.Setenv("METRICS_ADDR_FILE", "")
	for _, tc := range []struct {
		addr, want string
		invalid    bool
	}{
		{"", ":9464", false}, {" off ", "off", false}, {"OFF", "off", false}, {" OfF ", "off", false},
		{":0", ":0", false}, {":65535", ":65535", false}, {" 127.0.0.1:9464 ", "127.0.0.1:9464", false},
		{"[::1]:9464", "[::1]:9464", false}, {"[fe80::1%lo0]:9464", "[fe80::1%lo0]:9464", false},
		{"unresolved.invalid:9464", "unresolved.invalid:9464", false},
		{" ", "", true}, {"9464", "", true}, {"localhost", "", true}, {"::1:9464", "", true},
		{"localhost:", "", true}, {":http", "", true}, {":-1", "", true}, {":+1", "", true},
		{":65536", "", true}, {":999999999999999999999999999", "", true}, {"[::1:9464", "", true},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			t.Setenv("METRICS_ADDR", tc.addr)
			cfg, err := Load()
			if tc.invalid {
				if err == nil || !strings.Contains(err.Error(), "METRICS_ADDR") {
					t.Fatalf("invalid address accepted: %v", err)
				}
			} else if err != nil || cfg.MetricsAddr != tc.want {
				t.Fatalf("address=%q cfg=%v err=%v", tc.addr, cfg, err)
			}
		})
	}
}

func TestMetricsAddrHostSyntax(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example.invalid/learnd")
	t.Setenv("API_TOKEN", "test-token")
	t.Setenv("METRICS_ADDR_FILE", "")
	longest := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	valid := []string{
		":9464", "localhost:9464", "carma-1.internal:9464", "UNRESOLVED.invalid.:9464",
		"1carma.internal:9464", "xn--caf-dma.invalid:9464", "127.0.0.1:9464", "0.0.0.0:9464", "255.255.255.255:9464",
		"[::]:9464", "[2001:db8::1]:9464", "[::ffff:192.0.2.1]:9464", "[fe80::1%lo0]:9464",
		"[fe80::1%12]:9464", "[fe80::1%eth0.1]:9464", "[fe80::1%en_test-0]:9464",
		// Zones are platform interface identifiers, not DNS labels or URL paths.
		"[fe80::1%Local Area Connection]:9464", "[fe80::1%Loopback Pseudo-Interface 1]:9464",
		"[fe80::1%Connexion réseau]:9464", "[fe80::1%vEthernet (Default Switch)]:9464",
		"[fe80::1%#]:9464", "[fe80::1%&]:9464",
		longest + ":9464", longest + ".:9464",
	}
	invalid := []string{
		"bad host:9464", "foo/bar:9464", "999.999.999.999:9464", "256.1.1.1:9464", "127.000.0.1:9464",
		"127.1:9464", "127.0.0.1.:9464", "host\tname:9464", "host\nname:9464", "foo\\bar:9464",
		"user@host:9464", "host_name:9464", "-host:9464", "host-:9464", "host..name:9464", ".host:9464", "host..:9464", ".:9464",
		"[localhost]:9464", "[127.0.0.1]:9464", "[]:9464", "[gggg::1]:9464", "[::ffff:999.1.1.1]:9464",
		"[fe80::1%]:9464", "[gggg::1%Local Area Connection]:9464",
		"127.0.0.1%lo0:9464", "café.invalid:9464", strings.Repeat("a", 64) + ".invalid:9464", longest + "e:9464",
	}
	for _, addr := range valid {
		t.Run("valid/"+addr, func(t *testing.T) {
			t.Setenv("METRICS_ADDR", addr)
			cfg, err := Load()
			if err != nil || cfg.MetricsAddr != addr {
				t.Fatalf("address=%q cfg=%v err=%v", addr, cfg, err)
			}
		})
	}
	for _, addr := range invalid {
		t.Run("invalid/"+addr, func(t *testing.T) {
			t.Setenv("METRICS_ADDR", addr)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "METRICS_ADDR") {
				t.Fatalf("invalid host accepted: %q err=%v", addr, err)
			}
		})
	}
}

func TestMetricsAddrUnset(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example.invalid/learnd")
	t.Setenv("API_TOKEN", "test-token")
	t.Setenv("METRICS_ADDR_FILE", "")
	t.Setenv("METRICS_ADDR", "")
	if err := os.Unsetenv("METRICS_ADDR"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil || cfg.MetricsAddr != ":9464" {
		t.Fatalf("unset metrics address: cfg=%v err=%v", cfg, err)
	}
}
