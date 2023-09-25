// Package grep implements a subset of GNU grep: -i -v -n -r -c, plus -E
// (extended regexps), -F (fixed strings), -q (quiet) and -e PATTERN.
package grep

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/AbdeldjalilChougui/unix-tools/internal/cli"
)

var spec = cli.Spec{
	Bool:  "ivnrcEFqRH",
	Value: "e",
	Long: map[string]string{
		"ignore-case": "i", "invert-match": "v", "line-number": "n", "recursive": "r",
		"count": "c", "extended-regexp": "E", "fixed-strings": "F", "quiet": "q", "silent": "q",
		"regexp": "e", "with-filename": "H",
	},
}

type options struct {
	invert, number, recursive, count, quiet, withName bool
	re                                                *regexp.Regexp
}

// Main runs grep and returns its exit status: 0 if a line was selected,
// 1 if none, 2 on error (unless -q found a match).
func Main(args []string, env cli.Env) int {
	p, err := cli.Parse(spec, args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "grep: %v\n", err)
		return 2
	}
	patterns := p.Values["e"]
	if len(patterns) == 0 {
		if len(p.Args) == 0 {
			fmt.Fprintln(env.Stderr, "Usage: grep [OPTION]... PATTERNS [FILE]...")
			return 2
		}
		patterns = strings.Split(p.Args[0], "\n")
		p.Args = p.Args[1:]
	}
	re, err := compile(patterns, p.Has("E"), p.Has("F"), p.Has("i"))
	if err != nil {
		fmt.Fprintf(env.Stderr, "grep: %v\n", err)
		return 2
	}
	o := options{
		invert: p.Has("v"), number: p.Has("n"), recursive: p.Has("r") || p.Has("R"),
		count: p.Has("c"), quiet: p.Has("q"), re: re,
	}
	files := p.Args
	if len(files) == 0 {
		if o.recursive {
			files = []string{"."}
		} else {
			files = []string{"-"}
		}
	}
	o.withName = p.Has("H") || len(files) > 1 || o.recursive

	out := bufio.NewWriter(env.Stdout)
	defer out.Flush()
	matched, hadErr := false, false
	report := func(name string, err error) {
		hadErr = true
		out.Flush()
		fmt.Fprintf(env.Stderr, "grep: %s: %s\n", name, describe(err))
	}
	for _, f := range files {
		if f == "-" {
			if o.search(out, "(standard input)", env.Stdin) {
				matched = true
			}
			continue
		}
		info, err := os.Stat(env.Path(f))
		if err != nil {
			report(f, err)
			continue
		}
		if info.IsDir() {
			if !o.recursive {
				report(f, errors.New("Is a directory"))
				continue
			}
			// GNU grep -r with no operand prints paths without the "./".
			trim := len(p.Args) == 0
			for _, name := range walk(env.Path(f), f, report) {
				shown := name
				if trim {
					shown = strings.TrimPrefix(name, "./")
				}
				if o.searchFile(out, shown, env.Path(name), report) {
					matched = true
				}
			}
			continue
		}
		if o.searchFile(out, f, env.Path(f), report) {
			matched = true
		}
		if matched && o.quiet {
			return 0
		}
	}
	switch {
	case matched && o.quiet:
		return 0
	case hadErr:
		return 2
	case matched:
		return 0
	}
	return 1
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

// walk lists regular files under dir in lexical order. (GNU grep uses
// directory order, which is filesystem-dependent; sorting is deterministic.)
func walk(real, shown string, report func(string, error)) []string {
	var out []string
	filepath.WalkDir(real, func(path string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(real, path)
		name := filepath.Join(shown, rel)
		if shown == "." && rel != "." {
			name = "./" + rel
		}
		if err != nil {
			report(name, err)
			return nil
		}
		if d.Type().IsRegular() {
			out = append(out, name)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func (o *options) searchFile(out *bufio.Writer, shown, real string, report func(string, error)) bool {
	f, err := os.Open(real)
	if err != nil {
		report(shown, err)
		return false
	}
	defer f.Close()
	return o.search(out, shown, f)
}

// search scans r line by line and prints what GNU grep would.
func (o *options) search(out *bufio.Writer, name string, r io.Reader) bool {
	br := bufio.NewReaderSize(r, 64<<10)
	n, lineNo := 0, 0
	binary := false
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			lineNo++
			text := bytes.TrimSuffix(line, []byte("\n"))
			if bytes.IndexByte(text, 0) >= 0 {
				binary = true
			}
			if o.re.Match(text) != o.invert {
				n++
				if !o.count && !o.quiet && !binary {
					if o.withName {
						out.WriteString(name)
						out.WriteByte(':')
					}
					if o.number {
						fmt.Fprintf(out, "%d:", lineNo)
					}
					out.Write(text)
					out.WriteByte('\n')
				}
			}
		}
		if err != nil {
			break
		}
	}
	if o.count && !o.quiet {
		if o.withName {
			fmt.Fprintf(out, "%s:%d\n", name, n)
		} else {
			fmt.Fprintf(out, "%d\n", n)
		}
	} else if binary && n > 0 && !o.quiet {
		fmt.Fprintf(out, "Binary file %s matches\n", name)
	}
	return n > 0
}

// compile builds one regexp from the patterns (any of them may match).
func compile(patterns []string, extended, fixed, icase bool) (*regexp.Regexp, error) {
	parts := make([]string, len(patterns))
	for i, p := range patterns {
		switch {
		case fixed:
			parts[i] = regexp.QuoteMeta(p)
		case extended:
			parts[i] = p
		default:
			parts[i] = BREToRE2(p)
		}
	}
	expr := "(?:" + strings.Join(parts, ")|(?:") + ")"
	if icase {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}

// BREToRE2 translates a POSIX basic regular expression into Go syntax.
// In a BRE, + ? | ( ) { } are literal characters and their backslashed
// forms are the operators (a GNU extension for \+ \? \|). A '*' at the start
// of an expression or a group is literal too. Bracket expressions are copied
// unchanged.
func BREToRE2(s string) string {
	var b strings.Builder
	atStart := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			n := s[i+1]
			i++
			switch n {
			case '(', ')', '|', '{', '}', '+', '?':
				b.WriteByte(n)
				atStart = n == '(' || n == '|'
				continue
			default:
				b.WriteByte('\\')
				b.WriteByte(n)
			}
		case c == '[':
			j := i + 1
			if j < len(s) && s[j] == '^' {
				j++
			}
			if j < len(s) && s[j] == ']' {
				j++
			}
			for j < len(s) && s[j] != ']' {
				if s[j] == '[' && j+1 < len(s) && (s[j+1] == ':' || s[j+1] == '.' || s[j+1] == '=') {
					if k := strings.Index(s[j+2:], string(s[j+1])+"]"); k >= 0 {
						j += k + 4
						continue
					}
				}
				j++
			}
			if j >= len(s) {
				b.WriteString(`\[`)
			} else {
				b.WriteString(s[i : j+1])
				i = j
			}
		case strings.IndexByte("+?|(){}", c) >= 0:
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '*' && atStart:
			b.WriteString(`\*`)
		default:
			b.WriteByte(c)
		}
		atStart = c == '^' && atStart
	}
	return b.String()
}
