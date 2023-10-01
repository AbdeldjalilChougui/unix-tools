// Package gitignore matches paths against .gitignore rules as Git does:
// globs with * ? [..] and **, negation with !, directory-only patterns with
// a trailing /, anchoring by a leading or middle /, and nested .gitignore
// files whose rules apply to their own subtree and override their parents.
package gitignore

import (
	"bufio"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

// Rule is one pattern line.
type Rule struct {
	Pattern string
	Negate  bool
	DirOnly bool
	re      *regexp.Regexp
	base    bool // match against the basename only (no slash in pattern)
}

// ParseRules reads the lines of one .gitignore file.
func ParseRules(r io.Reader) []Rule {
	var out []Rule
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if rule, ok := parseLine(sc.Text()); ok {
			out = append(out, rule)
		}
	}
	return out
}

func parseLine(line string) (Rule, bool) {
	line = strings.TrimSuffix(line, "\r")
	// Trailing spaces are ignored unless escaped with a backslash.
	for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, `\ `) {
		line = line[:len(line)-1]
	}
	if line == "" || line[0] == '#' {
		return Rule{}, false
	}
	r := Rule{Pattern: line}
	if line[0] == '!' {
		r.Negate = true
		line = line[1:]
	} else if strings.HasPrefix(line, `\!`) || strings.HasPrefix(line, `\#`) {
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		r.DirOnly = true
		line = strings.TrimRight(line, "/")
	}
	if line == "" {
		return Rule{}, false
	}
	anchored := strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	r.base = !anchored
	re, err := regexp.Compile("^" + globToRegexp(line) + "$")
	if err != nil {
		return Rule{}, false
	}
	r.re = re
	return r, true
}

// globToRegexp converts a gitignore glob. "**" has special meaning only as a
// whole path segment: "**/x" (any leading dirs), "x/**" (everything inside),
// "a/**/b" (zero or more dirs in between).
func globToRegexp(g string) string {
	var b strings.Builder
	for i := 0; i < len(g); i++ {
		c := g[i]
		switch {
		case c == '*' && strings.HasPrefix(g[i:], "**"):
			atSegStart := i == 0 || g[i-1] == '/'
			atSegEnd := i+2 == len(g) || g[i+2] == '/'
			if atSegStart && atSegEnd {
				if i+2 == len(g) {
					b.WriteString(".*") // trailing /** : everything below
				} else {
					b.WriteString("(?:.*/)?") // **/ : zero or more dirs
					i++                       // skip the '/'
				}
				i++
				continue
			}
			b.WriteString("[^/]*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			j := i + 1
			if j < len(g) && (g[j] == '!' || g[j] == '^') {
				j++
			}
			if j < len(g) && g[j] == ']' {
				j++
			}
			for j < len(g) && g[j] != ']' {
				j++
			}
			if j >= len(g) {
				b.WriteString(`\[`)
				continue
			}
			class := g[i+1 : j]
			if class != "" && class[0] == '!' {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i = j
		case c == '\\' && i+1 < len(g):
			i++
			b.WriteString(regexp.QuoteMeta(string(g[i])))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// Match reports whether rel (slash-separated, relative to the rule file's
// directory) matches this rule.
func (r Rule) Match(rel string, isDir bool) bool {
	if r.DirOnly && !isDir {
		return false
	}
	if r.base {
		return r.re.MatchString(path.Base(rel))
	}
	return r.re.MatchString(rel)
}

// Matcher holds the rules of every .gitignore seen so far, keyed by the
// directory (relative to the root, "" for the root) that contains them.
type Matcher struct {
	sets map[string][]Rule
}

func New() *Matcher { return &Matcher{sets: map[string][]Rule{}} }

// AddFile loads dir/.gitignore if it exists. dir is relative to the root;
// real is the actual filesystem path of that directory.
func (m *Matcher) AddFile(dir, real string) {
	f, err := os.Open(path.Join(real, ".gitignore"))
	if err != nil {
		return
	}
	defer f.Close()
	m.Add(dir, ParseRules(f))
}

// Add registers rules for dir.
func (m *Matcher) Add(dir string, rules []Rule) {
	if len(rules) > 0 {
		m.sets[dir] = append(m.sets[dir], rules...)
	}
}

// Ignored reports whether rel (relative to the root) is ignored. Rules from
// deeper .gitignore files are applied after shallower ones, and within a
// file later lines win, so the last matching rule decides.
func (m *Matcher) Ignored(rel string, isDir bool) bool {
	ignored := false
	dirs := []string{""}
	for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
		dirs = append(dirs, d)
	}
	// dirs is [root, deepest, ..., shallowest]; apply root first then
	// shallow to deep.
	order := append([]string{dirs[0]}, reverse(dirs[1:])...)
	for _, d := range order {
		sub := rel
		if d != "" {
			sub = strings.TrimPrefix(rel, d+"/")
		}
		for _, r := range m.sets[d] {
			if r.Match(sub, isDir) {
				ignored = !r.Negate
			}
		}
	}
	return ignored
}

func reverse(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}
