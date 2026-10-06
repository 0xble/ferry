package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xble/toolkit/op"

	"github.com/0xble/ferry/share"
)

type ListInput struct{}

type IDInput struct {
	ID string `json:"id" arg:"" help:"Share ID"`
}

type RenewInput struct {
	ID  string `json:"id" arg:"" help:"Share ID"`
	For string `json:"for,omitempty" name:"for" default:"168h" help:"Additional lifetime from now"`
}

// Renewed is the renewed share. A preview is the share with the expiry the
// renewal would set and preview true.
type Renewed struct {
	share.ShareResponse
	Preview bool `json:"preview,omitempty"`
}

type UnshareInput struct {
	Target string `json:"target" arg:"" help:"Share ID or exact source path"`
}

// Unshared is the result of unshare: {id, ok} for a share revoked by ID, and
// {ok, path, revoked} for the shares of a path. A preview sets preview, with
// ok false, and lists the shares it would revoke in share_ids.
type Unshared struct {
	ID       string   `json:"id,omitempty"`
	OK       bool     `json:"ok"`
	Path     string   `json:"path,omitempty"`
	Preview  bool     `json:"preview,omitempty"`
	Revoked  int      `json:"revoked,omitempty"`
	ShareIDs []string `json:"share_ids,omitempty"`
}

func registerShares(reg *op.Registry, b *Backend) {
	op.Add(reg, op.Op[ListInput, []share.ShareResponse]{
		Name: "shares.list", CLI: "list", Summary: "List active shares", Effect: op.Read, MCP: true,
		Handler: func(ctx context.Context, req op.Request, in ListInput) ([]share.ShareResponse, error) {
			c := b.Client()
			if err := b.ensure(c); err != nil {
				return nil, err
			}
			shares, err := c.ListShares()
			if err != nil {
				return nil, daemonError(err)
			}
			if shares == nil {
				shares = []share.ShareResponse{}
			}
			return shares, nil
		},
		Render: func(w io.Writer, shares []share.ShareResponse) error {
			if len(shares) == 0 {
				_, err := fmt.Fprintln(w, "No active shares")
				return err
			}
			_, err := fmt.Fprintln(w, formatShareListText(shares))
			return err
		},
	})

	op.Add(reg, op.Op[IDInput, share.ShareResponse]{
		Name: "share.get", CLI: "get", Summary: "Get a share by id", Effect: op.Read, MCP: true,
		Handler: func(ctx context.Context, req op.Request, in IDInput) (share.ShareResponse, error) {
			c := b.Client()
			if err := b.ensure(c); err != nil {
				return share.ShareResponse{}, err
			}
			s, err := c.GetShare(strings.TrimSpace(in.ID))
			if err != nil {
				return share.ShareResponse{}, daemonError(err)
			}
			return s, nil
		},
		Render: renderShare,
	})

	op.Add(reg, op.Op[RenewInput, Renewed]{
		Name: "share.renew", CLI: "renew", Summary: "Extend share expiry", Effect: op.Write, CLIImmediate: true,
		Handler: func(ctx context.Context, req op.Request, in RenewInput) (Renewed, error) {
			ttl, err := lifetime("--for", in.For)
			if err != nil {
				return Renewed{}, err
			}
			id := strings.TrimSpace(in.ID)
			c := b.Client()
			if !req.Apply {
				if err := up(c); err != nil {
					return Renewed{}, err
				}
				s, err := c.GetShare(id)
				if err != nil {
					return Renewed{}, daemonError(err)
				}
				if s.Revoked {
					// The daemon renews only unrevoked shares.
					return Renewed{}, op.Errorf(op.KindNotFound, "not_found", "share not found")
				}
				s.ExpiresAt = b.Now().UTC().Add(ttl)
				return Renewed{ShareResponse: s, Preview: true}, nil
			}
			if err := b.ensure(c); err != nil {
				return Renewed{}, err
			}
			s, err := c.RenewShare(id, ttl)
			if err != nil {
				return Renewed{}, daemonError(err)
			}
			return Renewed{ShareResponse: s}, nil
		},
		Render: func(w io.Writer, r Renewed) error {
			if r.Preview {
				if _, err := fmt.Fprintln(w, "preview: renew would set this expiry (nothing changed)"); err != nil {
					return err
				}
			}
			return renderShare(w, r.ShareResponse)
		},
	})

	op.Add(reg, op.Op[UnshareInput, Unshared]{
		Name: "share.unshare", CLI: "unshare", Summary: "Revoke a share by id or exact path", Effect: op.Write, CLIImmediate: true,
		Handler: func(ctx context.Context, req op.Request, in UnshareInput) (Unshared, error) {
			c := b.Client()
			if req.Apply {
				if err := b.ensure(c); err != nil {
					return Unshared{}, err
				}
			} else if err := up(c); err != nil {
				return Unshared{}, err
			}
			target := strings.TrimSpace(in.Target)
			if target == "" {
				return Unshared{}, invalidArgs("share id or path is required")
			}
			return unshare(c, target, req.Apply)
		},
		Render: func(w io.Writer, u Unshared) error {
			var err error
			switch {
			case u.Preview && u.ID != "":
				_, err = fmt.Fprintf(w, "preview: would revoke share: %s\n", u.ID)
			case u.Preview:
				_, err = fmt.Fprintf(w, "preview: would revoke %d share(s) for %s: %s\n", len(u.ShareIDs), u.Path, strings.Join(u.ShareIDs, ", "))
			case u.ID != "":
				_, err = fmt.Fprintf(w, "revoked share: %s\n", u.ID)
			default:
				_, err = fmt.Fprintf(w, "revoked %d share(s) for %s\n", u.Revoked, u.Path)
			}
			return err
		},
	})
}

// unshare revokes the share whose ID is target, or else every active share
// whose path is target made absolute. Without apply it only finds them.
func unshare(c *share.Client, target string, apply bool) (Unshared, error) {
	if !strings.Contains(target, string(os.PathSeparator)) {
		var err error
		if apply {
			err = c.RevokeShare(target)
		} else {
			var s share.ShareResponse
			if s, err = c.GetShare(target); err == nil && s.Revoked {
				err = &share.APIError{StatusCode: 404, Code: "not_found", Message: "share not found"}
			}
		}
		if err == nil {
			if !apply {
				return Unshared{ID: target, Preview: true, ShareIDs: []string{target}}, nil
			}
			return Unshared{ID: target, OK: true}, nil
		}
		var apiErr *share.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "not_found" {
			return Unshared{}, daemonError(err)
		}
	}

	abs, err := filepath.Abs(target)
	if err != nil {
		return Unshared{}, err
	}
	shares, err := c.ListShares()
	if err != nil {
		return Unshared{}, daemonError(err)
	}

	var matched []string
	revoked := 0
	for _, s := range shares {
		if s.Path != abs {
			continue
		}
		matched = append(matched, s.ID)
		if apply {
			if err := c.RevokeShare(s.ID); err == nil {
				revoked++
			}
		}
	}
	if !apply && len(matched) > 0 {
		return Unshared{Path: abs, Preview: true, ShareIDs: matched}, nil
	}
	if revoked == 0 {
		return Unshared{}, op.Errorf(op.KindNotFound, "not_found", "no active share matched target")
	}
	return Unshared{OK: true, Path: abs, Revoked: revoked}, nil
}

func renderShare(w io.Writer, s share.ShareResponse) error {
	_, err := fmt.Fprintln(w, formatShareText(s, shareTextOptions{}))
	return err
}
