package share

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewDaemonDefaultsSnapshotMaxBytes(t *testing.T) {
	d := newTestDaemon(t)
	if d.cfg.SnapshotMaxBytes != DefaultSnapshotMaxBytes {
		t.Fatalf("snapshot max bytes = %d, want %d", d.cfg.SnapshotMaxBytes, DefaultSnapshotMaxBytes)
	}
}

func TestSnapshotShareRejectsConfiguredSizeLimit(t *testing.T) {
	d := newTestDaemon(t)
	d.cfg.SnapshotMaxBytes = 4

	source := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(source, []byte("12345"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	body, err := json.Marshal(CreateShareRequest{Path: source, Mode: ModeSnapshot})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	response := httptest.NewRecorder()
	d.handleAdminCreateShare(response, httptest.NewRequest(http.MethodPost, "/admin/share", bytes.NewReader(body)))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "use live mode") {
		t.Fatalf("error = %q, want live mode suggestion", response.Body.String())
	}
	entries, err := os.ReadDir(d.cfg.Paths.SnapshotsDir)
	if err != nil {
		t.Fatalf("read snapshots dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("snapshot directory created despite size refusal: %v", entries)
	}
}

func TestCreateShareFailureRemovesSnapshot(t *testing.T) {
	d := newTestDaemon(t)
	if err := d.store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	source := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(source, []byte("snapshot"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	body, err := json.Marshal(CreateShareRequest{
		Path: source,
		Mode: ModeSnapshot,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/share", bytes.NewReader(body))
	response := httptest.NewRecorder()

	d.handleAdminCreateShare(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}

	entries, err := os.ReadDir(d.cfg.Paths.SnapshotsDir)
	if err != nil {
		t.Fatalf("read snapshots dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("snapshot directories leaked after store failure: %v", entries)
	}
}

func TestRevokeShareSchedulesSnapshotCleanup(t *testing.T) {
	d := newTestDaemon(t)
	now := time.Now().UTC()
	share := Share{ID: "revoke-me", SourcePath: "/tmp/source", Mode: ModeSnapshot, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := d.store.CreateShare(share); err != nil {
		t.Fatalf("create share: %v", err)
	}
	snapshotDir := filepath.Join(d.cfg.Paths.SnapshotsDir, share.ID)
	if err := os.Mkdir(snapshotDir, 0o700); err != nil {
		t.Fatalf("mkdir snapshot: %v", err)
	}

	response := httptest.NewRecorder()
	d.handleAdminShareByID(response, httptest.NewRequest(http.MethodDelete, "/admin/shares/"+share.ID, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(snapshotDir); os.IsNotExist(err) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("snapshot directory still exists after revoke")
}

func TestGCSweepsOrphanAndInactiveSnapshotDirectories(t *testing.T) {
	d := newTestDaemon(t)
	now := time.Now().UTC()

	shares := []Share{
		{
			ID:         "active-snapshot",
			SourcePath: "/tmp/active",
			Mode:       ModeSnapshot,
			CreatedAt:  now,
			ExpiresAt:  now.Add(time.Hour),
		},
		{
			ID:         "revoked-snapshot",
			SourcePath: "/tmp/revoked",
			Mode:       ModeSnapshot,
			CreatedAt:  now,
			ExpiresAt:  now.Add(time.Hour),
		},
		{
			ID:         "expired-snapshot",
			SourcePath: "/tmp/expired",
			Mode:       ModeSnapshot,
			CreatedAt:  now.Add(-2 * time.Hour),
			ExpiresAt:  now.Add(-time.Minute),
		},
	}
	for _, share := range shares {
		if err := d.store.CreateShare(share); err != nil {
			t.Fatalf("create share %s: %v", share.ID, err)
		}
		if err := os.Mkdir(filepath.Join(d.cfg.Paths.SnapshotsDir, share.ID), 0o700); err != nil {
			t.Fatalf("mkdir snapshot %s: %v", share.ID, err)
		}
	}
	if err := d.store.RevokeShare("revoked-snapshot"); err != nil {
		t.Fatalf("revoke share: %v", err)
	}
	if err := os.Mkdir(filepath.Join(d.cfg.Paths.SnapshotsDir, "orphan"), 0o700); err != nil {
		t.Fatalf("mkdir orphan: %v", err)
	}

	d.gcSnapshots(now)

	for _, id := range []string{"revoked-snapshot", "expired-snapshot", "orphan"} {
		if _, err := os.Stat(filepath.Join(d.cfg.Paths.SnapshotsDir, id)); !os.IsNotExist(err) {
			t.Fatalf("snapshot %s still exists, stat err=%v", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(d.cfg.Paths.SnapshotsDir, "active-snapshot")); err != nil {
		t.Fatalf("active snapshot removed or missing: %v", err)
	}
}
