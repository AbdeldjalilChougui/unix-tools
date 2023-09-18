// Command wc is the unix-tools version of wc. See the README.
package main

import (
	"os"

	"github.com/AbdeldjalilChougui/unix-tools/internal/cli"
	"github.com/AbdeldjalilChougui/unix-tools/internal/wc"
)

func main() {
	os.Exit(wc.Main(os.Args[1:], cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}))
}
