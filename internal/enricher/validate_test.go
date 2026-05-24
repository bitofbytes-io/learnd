package enricher

import (
	"context"
	"net"
	"testing"
)

func TestValidateFetchURLRejectsUnsafeDestinations(t *testing.T) {
	tests := []string{
		"ftp://example.com/file",
		"https://user:pass@example.com/path",
		"http://localhost:8080/path",
		"http://127.0.0.1/path",
		"http://10.0.0.1/path",
		"http://203.0.113.10/path",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			if got, err := validateFetchURL(context.Background(), raw); err == nil {
				t.Fatalf("validateFetchURL(%q) = %v, want error", raw, got)
			}
		})
	}
}

func TestSafeDialContextRejectsPrivateRemoteAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	conn, err := safeDialContext(context.Background(), "tcp", listener.Addr().String())
	if err == nil {
		_ = conn.Close()
		t.Fatal("safeDialContext returned nil error for localhost")
	}
	<-done
}
