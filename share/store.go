package share

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db     *sql.DB
	dbPath string
}

func OpenStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	store := &Store{
		db:     db,
		dbPath: dbPath,
	}
	if err := store.ensurePrivateState(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func initSchema(db *sql.DB) error {
	queries := []string{
		`PRAGMA journal_mode=WAL;`,
		`CREATE TABLE IF NOT EXISTS shares (
			id TEXT PRIMARY KEY,
			source_path TEXT NOT NULL,
			is_dir INTEGER NOT NULL,
			mode TEXT NOT NULL,
			snapshot_root TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			revoked_at INTEGER,
			last_served_at INTEGER
		);`,
		`CREATE INDEX IF NOT EXISTS idx_shares_expires_at ON shares(expires_at);`,
		`CREATE INDEX IF NOT EXISTS idx_shares_source_path ON shares(source_path);`,
		`CREATE INDEX IF NOT EXISTS idx_shares_revoked_at ON shares(revoked_at);`,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) CreateShare(share Share) error {
	_, err := s.db.Exec(
		`INSERT INTO shares (
			id, source_path, is_dir, mode, snapshot_root, created_at, expires_at, revoked_at, last_served_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		share.ID,
		share.SourcePath,
		boolToInt(share.IsDir),
		share.Mode,
		share.SnapshotRoot,
		share.CreatedAt.Unix(),
		share.ExpiresAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("create share: %w", err)
	}
	return s.ensurePrivateState()
}

func (s *Store) GetShare(id string) (Share, error) {
	row := s.db.QueryRow(
		`SELECT id, source_path, is_dir, mode, snapshot_root, created_at, expires_at, revoked_at, last_served_at
		 FROM shares WHERE id = ?`,
		id,
	)
	share, err := scanShare(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Share{}, ErrNotFound
		}
		return Share{}, fmt.Errorf("get share: %w", err)
	}
	return share, nil
}

func (s *Store) ListShares(activeOnly bool) ([]Share, error) {
	query := `SELECT id, source_path, is_dir, mode, snapshot_root, created_at, expires_at, revoked_at, last_served_at FROM shares`
	args := []any{}
	if activeOnly {
		now := time.Now().UTC().Unix()
		query += ` WHERE revoked_at IS NULL AND expires_at > ?`
		args = append(args, now)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list shares: %w", err)
	}
	defer rows.Close()

	shares := []Share{}
	for rows.Next() {
		share, err := scanShare(rows)
		if err != nil {
			return nil, fmt.Errorf("scan share: %w", err)
		}
		shares = append(shares, share)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list shares rows: %w", err)
	}
	return shares, nil
}

func (s *Store) RevokeShare(id string) error {
	result, err := s.db.Exec(`UPDATE shares SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, time.Now().UTC().Unix(), id)
	if err != nil {
		return fmt.Errorf("revoke share: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke share rows: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return s.ensurePrivateState()
}

func (s *Store) RevokeSharesByPath(absPath string) (int64, error) {
	result, err := s.db.Exec(`UPDATE shares SET revoked_at = ? WHERE source_path = ? AND revoked_at IS NULL`, time.Now().UTC().Unix(), absPath)
	if err != nil {
		return 0, fmt.Errorf("revoke by path: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("revoke by path rows: %w", err)
	}
	return count, nil
}

func (s *Store) RenewShare(id string, expiresAt time.Time) error {
	result, err := s.db.Exec(`UPDATE shares SET expires_at = ? WHERE id = ? AND revoked_at IS NULL`, expiresAt.Unix(), id)
	if err != nil {
		return fmt.Errorf("renew share: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("renew share rows: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return s.ensurePrivateState()
}

func (s *Store) TouchLastServed(id string, ts time.Time) error {
	_, err := s.db.Exec(`UPDATE shares SET last_served_at = ? WHERE id = ?`, ts.Unix(), id)
	if err != nil {
		return fmt.Errorf("touch last served: %w", err)
	}
	return s.ensurePrivateState()
}

func (s *Store) ensurePrivateState() error {
	if s == nil {
		return nil
	}
	if err := ensureDirMode(filepath.Dir(s.dbPath), privateDirMode); err != nil {
		return fmt.Errorf("lock db dir: %w", err)
	}
	for _, path := range []string{
		s.dbPath,
		s.dbPath + "-shm",
		s.dbPath + "-wal",
	} {
		if err := EnsurePrivateFile(path); err != nil {
			return fmt.Errorf("lock db file %s: %w", path, err)
		}
	}
	return nil
}

func (s *Store) ExpiredShares(now time.Time) ([]Share, error) {
	rows, err := s.db.Query(
		`SELECT id, source_path, is_dir, mode, snapshot_root, created_at, expires_at, revoked_at, last_served_at
		 FROM shares
		 WHERE (revoked_at IS NOT NULL OR expires_at <= ?)`,
		now.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("query expired shares: %w", err)
	}
	defer rows.Close()

	shares := []Share{}
	for rows.Next() {
		share, err := scanShare(rows)
		if err != nil {
			return nil, fmt.Errorf("scan expired share: %w", err)
		}
		shares = append(shares, share)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("expired shares rows: %w", err)
	}
	return shares, nil
}

func scanShare(scanner interface{ Scan(dest ...any) error }) (Share, error) {
	var (
		id          string
		sourcePath  string
		isDir       int
		mode        string
		snapshot    string
		createdUnix int64
		expiresUnix int64
		revokedUnix sql.NullInt64
		servedUnix  sql.NullInt64
	)

	if err := scanner.Scan(
		&id,
		&sourcePath,
		&isDir,
		&mode,
		&snapshot,
		&createdUnix,
		&expiresUnix,
		&revokedUnix,
		&servedUnix,
	); err != nil {
		return Share{}, err
	}

	share := Share{
		ID:           id,
		SourcePath:   sourcePath,
		IsDir:        isDir == 1,
		Mode:         mode,
		SnapshotRoot: snapshot,
		CreatedAt:    time.Unix(createdUnix, 0).UTC(),
		ExpiresAt:    time.Unix(expiresUnix, 0).UTC(),
	}
	if revokedUnix.Valid {
		t := time.Unix(revokedUnix.Int64, 0).UTC()
		share.RevokedAt = &t
	}
	if servedUnix.Valid {
		t := time.Unix(servedUnix.Int64, 0).UTC()
		share.LastServedAt = &t
	}

	return share, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
