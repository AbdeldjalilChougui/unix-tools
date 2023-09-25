// Command grep is the unix-tools version of grep. See the README.
package main

import (
	"os"

	"github.com/AbdeldjalilChougui/unix-tools/internal/cli"
	"github.com/AbdeldjalilChougui/unix-tools/internal/grep"
)

func main() {
	os.Exit(grep.Main(os.Args[1:], cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}))
}
