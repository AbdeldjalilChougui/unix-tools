// Package jqlite is a small jq: paths (.a.b, .[0], .[]), pipes, comma,
// comparisons and arithmetic, array/object construction, and the builtins
// select, map, keys, keys_unsorted, length, has, type, not, add, sort,
// reverse, tostring, tonumber, to_entries and empty.
package jqlite

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AbdeldjalilChougui/unix-tools/internal/cli"
)

var spec = cli.Spec{
	Bool: "crn",
	Long: map[string]string{"compact-output": "c", "raw-output": "r", "null-input": "n"},
}

// Main runs jqlite. Exit codes follow jq: 2 usage, 3 compile error,
// 5 runtime error, 2 for bad JSON input.
func Main(args []string, env cli.Env) int {
	p, err := cli.Parse(spec, args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "jqlite: %v\n", err)
		return 2
	}
	if len(p.Args) == 0 {
		fmt.Fprintln(env.Stderr, "Usage: jqlite [-c] [-r] [-n] FILTER [FILE...]")
		return 2
	}
	prog, err := Parse(p.Args[0])
	if err != nil {
		fmt.Fprintf(env.Stderr, "jqlite: error: %v\njqlite: 1 compile error\n", err)
		return 3
	}
	pretty, raw := !p.Has("c"), p.Has("r")

	out := bufio.NewWriter(env.Stdout)
	defer out.Flush()
	status := 0
	run := func(v any) bool {
		res, err := Eval(prog, v)
		for _, r := range res {
			if s, ok := r.(string); ok && raw {
				out.WriteString(s)
			} else {
				var sb strings.Builder
				Encode(&sb, r, 0, pretty)
				out.WriteString(sb.String())
			}
			out.WriteByte('\n')
		}
		if err != nil {
			out.Flush()
			fmt.Fprintf(env.Stderr, "jqlite: error: %v\n", err)
			status = 5
		}
		return true
	}
	if p.Has("n") {
		run(nil)
		return status
	}
	var inputs []io.Reader
	if len(p.Args) == 1 {
		inputs = []io.Reader{env.Stdin}
	}
	for _, f := range p.Args[1:] {
		fh, err := os.Open(env.Path(f))
		if err != nil {
			fmt.Fprintf(env.Stderr, "jqlite: error: Could not open %s: %v\n", f, err)
			status = 2
			continue
		}
		defer fh.Close()
		inputs = append(inputs, fh)
	}
	dec := NewDecoder(io.MultiReader(inputs...))
	for {
		v, err := dec.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			out.Flush()
			fmt.Fprintf(env.Stderr, "jqlite: error (at input): %v\n", err)
			return 2
		}
		run(v)
	}
	return status
}
