package migrate

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/drywaters/learnd/internal/testdb"
	"github.com/pressly/goose/v3/lock"
)

func TestSIGTERMDuringMigrationExitsNonzero(t *testing.T) {
	pool := testdb.NewEmpty(t)
	// Keep this process-level test in the migrate package so its advisory-lock
	// contention cannot race other migration tests in a parallel test package.
	binary := filepath.Join(t.TempDir(), "learnd")
	buildCtx, stopBuild := context.WithTimeout(context.Background(), time.Minute)
	defer stopBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", binary, "./cmd/learnd") // #nosec G204 -- fixed local source; only output path varies within t.TempDir.
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build startup test binary: %v: %s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	holder, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	if _, err := holder.Exec(ctx, "SELECT pg_advisory_lock($1)", lock.DefaultLockID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = holder.Exec(cleanupCtx, "SELECT pg_advisory_unlock($1)", lock.DefaultLockID)
	}()
	cfg := pool.Config()
	command := exec.CommandContext(ctx, binary) // #nosec G204 -- binary was built immediately above from this repository into t.TempDir.
	// Only synthetic app credentials and the disposable database are passed to
	// the child; optional integrations cannot read local or deployment secrets.
	missing := filepath.Join(t.TempDir(), "not-configured")
	command.Env = []string{
		"PGAPPNAME=learnd_startup_signal_test",
		"PGOPTIONS=-c search_path=" + cfg.ConnConfig.RuntimeParams["search_path"],
		"DATABASE_URL=" + cfg.ConnConfig.ConnString(),
		"API_TOKEN=startup-test-only",
		"GEMINI_API_KEY_FILE=" + missing,
		"YOUTUBE_API_KEY_FILE=" + missing,
		"METRICS_ADDR=off",
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND application_name = 'learnd_startup_signal_test' AND query LIKE 'SELECT pg_try_advisory_lock%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("child did not reach migration lock waiting")
		case <-ticker.C:
		}
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	waited = true
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("startup exit = %v, want exit code 1", err)
	}
	if !strings.Contains(output.String(), "migrate: startup interrupted: context canceled") {
		t.Fatal("startup did not log a clear cancellation reason")
	}
	if strings.Contains(output.String(), cfg.ConnConfig.ConnString()) || strings.Contains(output.String(), "startup-test-only") {
		t.Fatal("startup log included credentials")
	}
}
