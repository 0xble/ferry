package share

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (d *Daemon) handleAdminHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"admin_addr":        d.cfg.AdminAddr,
		"public_base_url":   d.PublicBaseURL(),
		"external_base_url": d.ExternalBaseURL(),
	})
}

func (d *Daemon) handleAdminCreateShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var req CreateShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	target := strings.TrimSpace(req.Path)
	if target == "" {
		writeError(w, http.StatusBadRequest, "invalid_path", "path is required")
		return
	}

	absPath, err := filepath.Abs(target)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", err.Error())
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "not_found", "path not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "stat_error", err.Error())
		return
	}

	mode := ValidateMode(req.Mode)
	expiresIn := time.Duration(req.ExpiresInSeconds) * time.Second
	if expiresIn <= 0 {
		expiresIn = defaultShareTTL
	}

	now := time.Now().UTC()
	shareID, err := GenerateShareID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "id_generation_failed", err.Error())
		return
	}

	snapshotRoot := ""
	if mode == ModeSnapshot {
		snapshotRoot, err = CreateSnapshot(d.cfg.Paths, shareID, absPath, info.IsDir())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "snapshot_failed", err.Error())
			return
		}
	}

	share := Share{
		ID:           shareID,
		SourcePath:   absPath,
		IsDir:        info.IsDir(),
		Mode:         mode,
		SnapshotRoot: snapshotRoot,
		CreatedAt:    now,
		ExpiresAt:    now.Add(expiresIn),
	}

	if err := d.store.CreateShare(share); err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	writeJSON(w, http.StatusCreated, share.ToResponse(d.ExternalBaseURL(), token))
}

func (d *Daemon) handleAdminListShares(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	shares, err := d.store.ListShares(true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	res := make([]ShareResponse, 0, len(shares))
	for _, share := range shares {
		token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
		res = append(res, share.ToResponse(d.ExternalBaseURL(), token))
	}
	writeJSON(w, http.StatusOK, res)
}

func (d *Daemon) handleAdminShareByID(w http.ResponseWriter, r *http.Request) {
	tail := strings.TrimPrefix(r.URL.Path, "/admin/shares/")
	tail = strings.TrimSpace(tail)
	if tail == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "share id is required")
		return
	}

	if strings.HasSuffix(tail, "/renew") {
		id := strings.TrimSuffix(tail, "/renew")
		d.handleAdminRenewShare(w, r, id)
		return
	}

	id := tail
	switch r.Method {
	case http.MethodGet:
		share, err := d.store.GetShare(id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "share not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
		writeJSON(w, http.StatusOK, share.ToResponse(d.ExternalBaseURL(), token))
	case http.MethodDelete:
		if err := d.store.RevokeShare(id); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "share not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
			return
		}
		_ = os.RemoveAll(filepath.Join(d.cfg.Paths.SnapshotsDir, id))
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	default:
		methodNotAllowed(w)
	}
}

func (d *Daemon) handleAdminRenewShare(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var req RenewShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.ExpiresInSeconds <= 0 {
		req.ExpiresInSeconds = int64(defaultShareTTL / time.Second)
	}

	expiresAt := time.Now().UTC().Add(time.Duration(req.ExpiresInSeconds) * time.Second)
	if err := d.store.RenewShare(id, expiresAt); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "share not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}

	share, err := d.store.GetShare(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	token := ShareToken(d.secret, share.ID, d.cfg.TokenBytes)
	writeJSON(w, http.StatusOK, share.ToResponse(d.ExternalBaseURL(), token))
}
