package ops

import (
	"os"
	"strings"

	"github.com/0xble/toolkit/op"
)

// ImplicitPublish rewrites `ferry <path> ...` to `ferry publish <path> ...`.
// args exclude the program name. The first argument that is not a flag is
// left alone when it is a command word, and becomes a publish target only
// when it names an existing file or directory, so an unknown word stays a
// usage error. --fields is the only root flag that takes a separate value.
func ImplicitPublish(reg *op.Registry, args []string) []string {
	known := map[string]bool{"help": true, "serve": true, "mcp": true, "metadata": true}
	for _, e := range reg.Entries() {
		known[e.CLIPath[0]] = true
	}

	for idx := 0; idx < len(args); idx++ {
		token := strings.TrimSpace(args[idx])
		switch token {
		case "", "--", "-h", "--help":
			return args
		case "--fields":
			idx++
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		if known[token] {
			return args
		}
		if _, err := os.Stat(args[idx]); err != nil {
			return args
		}
		out := make([]string, 0, len(args)+1)
		out = append(out, args[:idx]...)
		out = append(out, "publish")
		return append(out, args[idx:]...)
	}
	return args
}
