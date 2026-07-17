package store

import (
	"context"
	"database/sql/driver"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type execQuerierWithoutFileControl struct{}

func (execQuerierWithoutFileControl) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, nil
}

func (execQuerierWithoutFileControl) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, nil
}

func TestPersistentWALSurvivesClose(t *testing.T) {
	dataDir := t.TempDir()
	cfg := mustDefaultConfig(t)
	cfg.DataDir = dataDir

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	dbPath := filepath.Join(dataDir, "engram.db")
	for _, suffix := range []string{"-wal", "-shm"} {
		info, err := os.Stat(dbPath + suffix)
		if err != nil {
			t.Errorf("stat %s: %v", suffix, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", suffix)
		}
	}
}

func TestConnectionReplacementPreservesPragmas(t *testing.T) {
	cfg := mustDefaultConfig(t)
	cfg.DataDir = t.TempDir()

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer s.Close()

	const cacheSizeMarker = -12345
	if _, err := s.db.Exec("PRAGMA cache_size = -12345"); err != nil {
		t.Fatalf("set cache_size marker: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var count int64
	err = s.db.QueryRowContext(ctx,
		`WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c) SELECT count(*) FROM c`,
	).Scan(&count)
	if err == nil {
		t.Fatal("expected interrupted query to fail")
	}

	var cacheSize int
	if err := s.db.QueryRow("PRAGMA cache_size").Scan(&cacheSize); err != nil {
		t.Fatalf("read cache_size: %v", err)
	}
	if cacheSize == cacheSizeMarker {
		t.Fatal("connection was not replaced")
	}

	var busyTimeout int
	if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
	}

	var journalMode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}
}

func TestPersistWALHookToleratesNonFileControlConnection(t *testing.T) {
	if err := enablePersistWAL(execQuerierWithoutFileControl{}); err != nil {
		t.Fatalf("enable persistent WAL: %v", err)
	}
}
