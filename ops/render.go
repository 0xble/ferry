package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/0xble/ferry/share"
)

type shareTextOptions struct {
	Path       string
	BundleRoot string
}

func formatShareText(shareResp share.ShareResponse, opts shareTextOptions) string {
	displayPath := shareResp.Path
	if strings.TrimSpace(opts.Path) != "" {
		displayPath = opts.Path
	}

	lines := []string{
		fmt.Sprintf("id: %s", shareResp.ID),
		fmt.Sprintf("kind: %s", shareKind(shareResp.IsDir)),
		fmt.Sprintf("mode: %s", shareResp.Mode),
		fmt.Sprintf("path: %s", displayPath),
	}
	if strings.TrimSpace(opts.BundleRoot) != "" {
		lines = append(lines, fmt.Sprintf("bundle_root: %s", opts.BundleRoot))
	}
	if !shareResp.CreatedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("created: %s", shareResp.CreatedAt.Local().Format(time.RFC3339)))
	}
	lines = append(lines,
		fmt.Sprintf("expires: %s", shareResp.ExpiresAt.Local().Format(time.RFC3339)),
		fmt.Sprintf("url: %s", shareResp.URL),
	)
	return strings.Join(lines, "\n")
}

func formatShareListText(shares []share.ShareResponse) string {
	entries := make([]string, 0, len(shares))
	for _, shareResp := range shares {
		entries = append(entries, formatShareText(shareResp, shareTextOptions{}))
	}
	return strings.Join(entries, "\n\n")
}

func shareKind(isDir bool) string {
	if isDir {
		return "directory"
	}
	return "file"
}
