package netguard

import (
	"net"
	"testing"
)

func TestIsLocalhost(t *testing.T) {
	for _, host := range []string{"localhost", "LOCALHOST.", "app.localhost"} {
		if !IsLocalhost(host) {
			t.Fatalf("IsLocalhost(%q) = false", host)
		}
	}
}

func TestIsUnsafeIP(t *testing.T) {
	tests := []struct {
		ip     string
		unsafe bool
	}{
		{ip: "127.0.0.1", unsafe: true},
		{ip: "10.0.0.1", unsafe: true},
		{ip: "100.64.0.1", unsafe: true},
		{ip: "169.254.1.1", unsafe: true},
		{ip: "203.0.113.10", unsafe: true},
		{ip: "::1", unsafe: true},
		{ip: "fc00::1", unsafe: true},
		{ip: "8.8.8.8", unsafe: false},
		{ip: "2001:4860:4860::8888", unsafe: false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := IsUnsafeIP(net.ParseIP(tt.ip)); got != tt.unsafe {
				t.Fatalf("IsUnsafeIP(%q) = %t, want %t", tt.ip, got, tt.unsafe)
			}
		})
	}
}
