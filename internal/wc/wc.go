// Package wc implements GNU wc's -l -w -c -m, including its column-width
// rules.
package wc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/AbdeldjalilChougui/unix-tools/internal/cli"
)

var spec = cli.Spec{
	Bool: "lwcm",
	Long: map[string]string{"lines": "l", "words": "w", "bytes": "c", "chars": "m"},
}

// Counts for one input.
type Counts struct {
	Lines, Words, Chars, Bytes int64
}

// Count reads r to the end. Words are maximal runs of non-space characters;
// bytes that are not valid UTF-8 count as part of a word (as in GNU wc).
func Count(r io.Reader) (Counts, error) {
	var c Counts
	br := bufio.NewReaderSize(r, 64<<10)
	inWord := false
	for {
		ru, size, err := br.ReadRune()
		if err != nil {
			if err == io.EOF {
				return c, nil
			}
			return c, err
		}
		c.Bytes += int64(size)
		if ru != utf8.RuneError || size > 1 {
			c.Chars++
		}
		if ru == '\n' {
			c.Lines++
		}
		if isSpace(ru) && !(ru == utf8.RuneError && size == 1) {
			inWord = false
		} else if !inWord {
			inWord = true
			c.Words++
		}
	}
}

// isSpace follows GNU wc 9: Unicode white space plus the no-break spaces.
func isSpace(r rune) bool {
	switch r {
	case 0xA0, 0x2007, 0x202F, 0x2060:
		return true
	}
	return unicode.IsSpace(r)
}

type input struct {
	name    string
	counts  Counts
	regular bool
	size    int64
	failed  bool
}

// Main runs wc.
func Main(args []string, env cli.Env) int {
	p, err := cli.Parse(spec, args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "wc: %v\n", err)
		return 1
	}
	show := struct{ l, w, m, c bool }{p.Has("l"), p.Has("w"), p.Has("m"), p.Has("c")}
	if !show.l && !show.w && !show.m && !show.c {
		show.l, show.w, show.c = true, true, true
	}
	files := p.Args
	named := len(files) > 0
	if !named {
		files = []string{"-"}
	}
	status := 0
	var ins []input
	for _, f := range files {
		in := input{name: f}
		var r io.Reader
		if f == "-" {
			r = env.Stdin
			if st, ok := env.Stdin.(*os.File); ok {
				if info, err := st.Stat(); err == nil && info.Mode().IsRegular() {
					in.regular, in.size = true, info.Size()
				}
			}
		} else {
			fh, err := os.Open(env.Path(f))
			if err == nil {
				var info fs.FileInfo
				if info, err = fh.Stat(); err == nil && info.IsDir() {
					err = errors.New("Is a directory")
				} else if err == nil {
					in.regular, in.size = info.Mode().IsRegular(), info.Size()
				}
			}
			if err != nil {
				fmt.Fprintf(env.Stderr, "wc: %s: %s\n", f, describe(err))
				in.failed, status = true, 1
				if fh != nil {
					fh.Close()
				}
				ins = append(ins, in)
				continue
			}
			defer fh.Close()
			r = fh
		}
		if in.counts, err = Count(r); err != nil {
			fmt.Fprintf(env.Stderr, "wc: %s: %v\n", f, err)
			in.failed, status = true, 1
		}
		ins = append(ins, in)
	}

	nShown := 0
	for _, b := range []bool{show.l, show.w, show.m, show.c} {
		if b {
			nShown++
		}
	}
	width := 1
	if !(nShown == 1 && len(ins) == 1) {
		width = numberWidth(ins)
	}
	out := bufio.NewWriter(env.Stdout)
	defer out.Flush()
	print := func(c Counts, name string) {
		var cols []string
		pad := func(n int64) string {
			s := strconv.FormatInt(n, 10)
			if len(s) < width {
				s = strings.Repeat(" ", width-len(s)) + s
			}
			return s
		}
		if show.l {
			cols = append(cols, pad(c.Lines))
		}
		if show.w {
			cols = append(cols, pad(c.Words))
		}
		if show.m {
			cols = append(cols, pad(c.Chars))
		}
		if show.c {
			cols = append(cols, pad(c.Bytes))
		}
		line := strings.Join(cols, " ")
		if name != "" {
			line += " " + name
		}
		out.WriteString(line + "\n")
	}
	var total Counts
	for _, in := range ins {
		if in.failed {
			continue
		}
		name := in.name
		if !named {
			name = ""
		}
		print(in.counts, name)
		total.Lines += in.counts.Lines
		total.Words += in.counts.Words
		total.Chars += in.counts.Chars
		total.Bytes += in.counts.Bytes
	}
	if len(ins) > 1 {
		print(total, "total")
	}
	return status
}

// numberWidth mirrors coreutils: the digits of the summed size of regular
// files, but at least 7 if any input is not a regular file (a pipe, for
// instance), since its size is unknown in advance.
func numberWidth(ins []input) int {
	minimum, total := 1, int64(0)
	for _, in := range ins {
		if in.failed {
			continue
		}
		if !in.regular {
			minimum = 7
		} else {
			total += in.size
		}
	}
	w := len(strconv.FormatInt(total, 10))
	return max(w, minimum)
}

func describe(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "No such file or directory"
	case errors.Is(err, fs.ErrPermission):
		return "Permission denied"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
