// Command tree is the unix-tools version of tree. See the README.
package main

import (
	"os"

	"github.com/AbdeldjalilChougui/unix-tools/internal/cli"
	"github.com/AbdeldjalilChougui/unix-tools/internal/tree"
)

func main() {
	os.Exit(tree.Main(os.Args[1:], cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}))
}
