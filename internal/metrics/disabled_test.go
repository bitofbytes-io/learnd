package metrics

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestDisabledAddressVariantsDoNotBind(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, addr := range []string{"off", "OFF", " OfF ", "\toFf\n"} {
		logs.Reset()
		if server := Start(addr, NewRegistry()); server != nil {
			t.Fatalf("%q started a listener", addr)
		}
		if !strings.Contains(logs.String(), "metrics listener disabled") || strings.Contains(logs.String(), "ERROR") {
			t.Fatalf("%q did not take the disabled path: %s", addr, logs.String())
		}
	}
}
