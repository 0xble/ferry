package share

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreCreateListRevokeRenew(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "shares.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Now().UTC()
	share := Share{
		ID:         "share123",
		SourcePath: "/tmp/file.md",
		IsDir:      false,
		Mode:       ModeLive,
		CreatedAt:  now,
		ExpiresAt:  now.Add(24 * time.Hour),
	}
	if err := store.CreateShare(share); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	active, err := store.ListShares(true)
	if err != nil {
		t.Fatalf("ListShares(active): %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active share, got %d", len(active))
	}

	if err := store.RenewShare(share.ID, now.Add(48*time.Hour)); err != nil {
		t.Fatalf("RenewShare: %v", err)
	}

	loaded, err := store.GetShare(share.ID)
	if err != nil {
		t.Fatalf("GetShare: %v", err)
	}
	if loaded.ExpiresAt.Before(now.Add(47 * time.Hour)) {
		t.Fatalf("expected renewed expiry, got %s", loaded.ExpiresAt)
	}

	if err := store.RevokeShare(share.ID); err != nil {
		t.Fatalf("RevokeShare: %v", err)
	}

	active, err = store.ListShares(true)
	if err != nil {
		t.Fatalf("ListShares(active) after revoke: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected 0 active shares after revoke, got %d", len(active))
	}
}
