// Command jqlite is the unix-tools version of jqlite. See the README.
package main

import (
	"os"

	"github.com/AbdeldjalilChougui/unix-tools/internal/cli"
	"github.com/AbdeldjalilChougui/unix-tools/internal/jqlite"
)

func main() {
	os.Exit(jqlite.Main(os.Args[1:], cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}))
}
