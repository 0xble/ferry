// Command ferry publishes local files and directories as tailnet-only links
// through the ferryd daemon. Its operations are declared in ops.
package main

import (
	"os"

	"github.com/0xble/toolkit"

	"github.com/0xble/ferry/ops"
)

var version = "dev"

func main() {
	b := ops.NewBackend()
	reg := ops.New(version, b)
	os.Args = append(os.Args[:1:1], ops.ImplicitPublish(reg, os.Args[1:])...)
	toolkit.Main(reg, ops.Options(b))
}
