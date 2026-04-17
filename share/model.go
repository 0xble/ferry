package share

import "time"

const (
	ModeLive     = "live"
	ModeSnapshot = "snapshot"
)

type Share struct {
	ID           string
	SourcePath   string
	IsDir        bool
	Mode         string
	SnapshotRoot string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	RevokedAt    *time.Time
	LastServedAt *time.Time
}

func (s Share) IsActive(now time.Time) bool {
	if s.RevokedAt != nil {
		return false
	}
	return s.ExpiresAt.After(now)
}

type CreateShareRequest struct {
	Path             string `json:"path"`
	Mode             string `json:"mode"`
	ExpiresInSeconds int64  `json:"expires_in_seconds"`
}

type RenewShareRequest struct {
	ExpiresInSeconds int64 `json:"expires_in_seconds"`
}

type ShareResponse struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	IsDir     bool      `json:"is_dir"`
	Mode      string    `json:"mode"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
}

func (s Share) ToResponse(baseURL string, token string) ShareResponse {
	preview := baseURL + "/s/" + s.ID + "?t=" + token
	if s.IsDir {
		preview = baseURL + "/s/" + s.ID + "/?t=" + token
	}

	return ShareResponse{
		ID:        s.ID,
		Path:      s.SourcePath,
		IsDir:     s.IsDir,
		Mode:      s.Mode,
		URL:       preview,
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.ExpiresAt,
		Revoked:   s.RevokedAt != nil,
	}
}
