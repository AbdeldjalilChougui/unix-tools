// Package cli has a tiny getopt used by all the tools, so that they accept
// GNU-style arguments: clustered short flags (-rn), flags after operands
// (grep foo -i file), "--" to end options, and long --flags.
package cli

import (
	"fmt"
	"io"
	"strings"
)

// Spec describes the options a tool accepts.
type Spec struct {
	Bool  string            // short boolean flags, e.g. "ivnrc"
	Value string            // short flags that take a value, e.g. "eL"
	Long  map[string]string // long name → short letter (or itself if not short)
	// LongValue lists long names that take a value (--level=2 or --level 2).
	LongValue map[string]bool
}

// Parsed holds the result.
type Parsed struct {
	Flags  map[string]int      // count of each boolean flag
	Values map[string][]string // values of value flags, in order
	Args   []string            // operands
}

func (p *Parsed) Has(f string) bool { return p.Flags[f] > 0 }

func (p *Parsed) Last(f string) (string, bool) {
	v := p.Values[f]
	if len(v) == 0 {
		return "", false
	}
	return v[len(v)-1], true
}

// Parse parses args (without the program name).
func Parse(spec Spec, args []string) (*Parsed, error) {
	p := &Parsed{Flags: map[string]int{}, Values: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			p.Args = append(p.Args, args[i+1:]...)
			return p, nil
		case strings.HasPrefix(a, "--"):
			name, val, hasVal := strings.Cut(a[2:], "=")
			key, ok := spec.Long[name]
			if !ok {
				return nil, fmt.Errorf("unrecognized option '--%s'", name)
			}
			if spec.LongValue[name] || strings.ContainsRune(spec.Value, rune(key[0])) && len(key) == 1 {
				if !hasVal {
					if i+1 >= len(args) {
						return nil, fmt.Errorf("option '--%s' requires an argument", name)
					}
					i++
					val = args[i]
				}
				p.Values[key] = append(p.Values[key], val)
			} else {
				if hasVal {
					return nil, fmt.Errorf("option '--%s' doesn't allow an argument", name)
				}
				p.Flags[key]++
			}
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				c := string(a[j])
				switch {
				case strings.Contains(spec.Bool, c):
					p.Flags[c]++
				case strings.Contains(spec.Value, c):
					val := a[j+1:]
					if val == "" {
						if i+1 >= len(args) {
							return nil, fmt.Errorf("option requires an argument -- '%s'", c)
						}
						i++
						val = args[i]
					}
					p.Values[c] = append(p.Values[c], val)
					j = len(a)
				default:
					return nil, fmt.Errorf("invalid option -- '%s'", c)
				}
			}
		default:
			p.Args = append(p.Args, a)
		}
	}
	return p, nil
}

// Env is what a tool's Main receives instead of touching os.* directly, so
// tests can run it in-process.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Dir    string // working directory for relative paths ("" = process cwd)
}

// Path resolves p against Dir for opening; output should keep p as given.
func (e Env) Path(p string) string {
	if e.Dir == "" || len(p) > 0 && p[0] == '/' {
		return p
	}
	return e.Dir + "/" + p
}
