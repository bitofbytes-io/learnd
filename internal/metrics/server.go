package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Disabled is the METRICS_ADDR value that turns the metrics listener off.
const Disabled = "off"

// Server is the internal metrics listener. A nil *Server is valid and does
// nothing, which is what Start returns when metrics are off or unavailable.
type Server struct {
	http *http.Server
	addr net.Addr
}

// Start serves /metrics from reg on addr in the background. It returns nil
// when addr is Disabled or the address cannot be bound; a bind failure is
// logged rather than returned so metrics never stop learnd from serving.
func Start(addr string, reg *prometheus.Registry) *Server {
	addr = strings.TrimSpace(addr)
	if strings.EqualFold(addr, Disabled) {
		slog.Info("metrics listener disabled")
		return nil
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("metrics listener unavailable", "addr", addr, "error", err)
		return nil
	}
	srv := &http.Server{
		Handler:           Handler(reg),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		slog.Info("metrics listener listening", "addr", listener.Addr().String())
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics listener stopped", "error", err)
		}
	}()
	return &Server{http: srv, addr: listener.Addr()}
}

// Addr reports the bound address, which differs from the requested one when
// the port is 0.
func (s *Server) Addr() net.Addr {
	return s.addr
}

// Shutdown gracefully stops the listener.
func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}
