package gitignore

import (
	"strings"
	"testing"
)

func TestRules(t *testing.T) {
	m := New()
	m.Add("", ParseRules(strings.NewReader(`
# comment
*.log
!keep.log
build/
/root-only.txt
docs/**/draft*.md
**/cache
a/**
\#literal
trailing-space.txt   
`)))
	m.Add("sub", ParseRules(strings.NewReader("*.tmp\n!important.log\n")))
	cases := []struct {
		path string
		dir  bool
		want bool
	}{
		{"x.log", false, true},
		{"deep/x.log", false, true},
		{"keep.log", false, false},
		{"build", true, true},
		{"build", false, false}, // trailing slash: directories only
		{"src/build", true, true},
		{"root-only.txt", false, true},
		{"sub/root-only.txt", false, false}, // anchored to the root
		{"docs/draft1.md", false, true},
		{"docs/a/b/draft2.md", false, true},
		{"docs/final.md", false, false},
		{"cache", true, true},
		{"x/y/cache", true, true},
		{"a/anything/below", false, true},
		{"a", true, false}, // "a/**" matches inside a, not a itself
		{"#literal", false, true},
		{"trailing-space.txt", false, true},
		{"sub/x.tmp", false, true},
		{"x.tmp", false, false},             // sub/.gitignore only applies under sub/
		{"sub/important.log", false, false}, // deeper file overrides parent
		{"sub/other.log", false, true},
	}
	for _, c := range cases {
		if got := m.Ignored(c.path, c.dir); got != c.want {
			t.Errorf("Ignored(%q, dir=%v) = %v, want %v", c.path, c.dir, got, c.want)
		}
	}
}

func TestGlobToRegexp(t *testing.T) {
	for glob, want := range map[string]string{
		"*.go":    `[^/]*\.go`,
		"a?c":     `a[^/]c`,
		"[!a-c]x": `[^a-c]x`,
		"**/x":    `(?:.*/)?x`,
		"x/**":    `x/.*`,
		"a**b":    `a[^/]*b`,
	} {
		if got := globToRegexp(glob); got != want {
			t.Errorf("%s: got %s want %s", glob, got, want)
		}
	}
}
