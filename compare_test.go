package unixtools_test

// These table tests run each tool in-process and compare its stdout and exit
// status with the real tool:
//
//   - If the real tool is available (grep and wc on PATH; jq and tree on PATH
//     or via $JQ_BIN / $TREE_BIN), its output is the expectation.
//   - Either way the output is also checked against testdata/golden/*.txt,
//     recorded from the real tools (GNU grep 3.12, GNU wc 9.x, jq 1.7.1,
//     tree 2.2.1), so the tests mean something on machines without jq or
//     tree. `go test -update` re-records them from whichever real tools are
//     present.

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/AbdeldjalilChougui/unix-tools/internal/cli"
	"github.com/AbdeldjalilChougui/unix-tools/internal/grep"
	"github.com/AbdeldjalilChougui/unix-tools/internal/jqlite"
	"github.com/AbdeldjalilChougui/unix-tools/internal/tree"
	"github.com/AbdeldjalilChougui/unix-tools/internal/wc"
)

var update = flag.Bool("update", false, "re-record golden files from the real tools")

// fixture is created fresh in a temp dir for every run. (It can't live in
// testdata as-is: its .gitignore files would apply to this repository.)
var fixture = map[string]string{
	".gitignore":               "# build output\n*.log\nbuild/\n!keep.log\nsecret*\n/docs/draft.md\n",
	"README.md":                "# Demo\n\nhello world\nHello again\nfoo bar baz\n",
	"notes.txt":                "hello\nfoo\nfoobar\nfooo bar\n  indented hello\na(b|c)\nax+y\nabc\nTODO: write more\n",
	"app.log":                  "log line foo\n",
	"keep.log":                 "kept foo\n",
	"secrets.txt":              "password=hunter2\n",
	"build/out.txt":            "compiled foo\n",
	"docs/guide.md":            "Guide\nTODO: screenshots\n",
	"docs/draft.md":            "draft foo\n",
	"docs/.hidden":             "hidden foo\n",
	"src/main.go":              "package main\n\nfunc main() {\n\tprintln(\"foo\")\n}\n",
	"src/util/strings.go":      "package util\n\n// TODO: more helpers\nfunc Upper(s string) string { return s }\n",
	"src/util/.gitignore":      "*_test.go\n",
	"src/util/strings_test.go": "package util\n",
	"unicode.txt":              "héllo wörld\n日本語 テキスト\nno\u00a0break\n",
	"data.json": `{
  "users": [
    {"name": "Amina", "age": 34, "city": "Oran", "tags": ["admin", "dev"]},
    {"name": "Karim", "age": 17, "city": "Algiers", "tags": []},
    {"name": "Lina", "age": 41, "city": "Constantine", "tags": ["dev"], "email": null}
  ],
  "meta": {"version": "1.0", "count": 3, "ratio": 0.10, "big": 1e2, "pi": 3.14159,
           "nested": {"z": 1, "a": {"deep": true}}},
  "weird": "tab\there \"quoted\" back\\slash \u00e9 \u0001",
  "empty": {}, "list": []
}
`,
}

func makeFixture(t *testing.T) string {
	dir := t.TempDir()
	for name, content := range fixture {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type tcase struct {
	name      string
	tool      string // grep | wc | jq | tree
	args      []string
	stdin     string
	sortLines bool // real grep -r walks in directory order, which varies
	locale    string
}

var cases = []tcase{
	// grep
	{name: "grep-basic", tool: "grep", args: []string{"hello", "notes.txt"}},
	{name: "grep-i", tool: "grep", args: []string{"-i", "HELLO", "notes.txt", "README.md"}},
	{name: "grep-vn", tool: "grep", args: []string{"-vn", "foo", "notes.txt"}},
	{name: "grep-c", tool: "grep", args: []string{"-c", "foo", "notes.txt", "README.md", "src/main.go"}},
	{name: "grep-c-single", tool: "grep", args: []string{"-c", "o", "notes.txt"}},
	{name: "grep-rn", tool: "grep", args: []string{"-rn", "TODO", "src", "docs"}, sortLines: true},
	{name: "grep-r-noarg", tool: "grep", args: []string{"-r", "TODO"}, sortLines: true},
	{name: "grep-r-dot", tool: "grep", args: []string{"-ri", "todo", "."}, sortLines: true},
	{name: "grep-bre-group", tool: "grep", args: []string{`fo\(o\|b\)`, "notes.txt"}},
	{name: "grep-bre-literal", tool: "grep", args: []string{"a(b|c)", "notes.txt"}},
	{name: "grep-bre-plus", tool: "grep", args: []string{`x+`, "notes.txt"}},
	{name: "grep-bre-interval", tool: "grep", args: []string{`o\{3\}`, "notes.txt"}},
	{name: "grep-ere", tool: "grep", args: []string{"-E", "fo+ |a(b|c)", "notes.txt"}},
	{name: "grep-fixed", tool: "grep", args: []string{"-F", "x+y", "notes.txt"}},
	{name: "grep-anchors", tool: "grep", args: []string{"-n", "^foo", "notes.txt"}},
	{name: "grep-class", tool: "grep", args: []string{"[[:space:]]hello", "notes.txt"}},
	{name: "grep-multi-e", tool: "grep", args: []string{"-e", "abc", "-e", "hello$", "notes.txt"}},
	{name: "grep-stdin", tool: "grep", args: []string{"-n", "b"}, stdin: "a\nb\ncb\n"},
	{name: "grep-nomatch", tool: "grep", args: []string{"zzz", "notes.txt"}},
	{name: "grep-missing", tool: "grep", args: []string{"foo", "nope.txt", "notes.txt"}},
	{name: "grep-dir-no-r", tool: "grep", args: []string{"foo", "src"}},
	{name: "grep-flags-after", tool: "grep", args: []string{"hello", "notes.txt", "-c"}},
	{name: "grep-q", tool: "grep", args: []string{"-q", "foo", "notes.txt"}},

	// wc
	{name: "wc-default", tool: "wc", args: []string{"notes.txt"}},
	{name: "wc-l-one", tool: "wc", args: []string{"-l", "notes.txt"}},
	{name: "wc-multi", tool: "wc", args: []string{"notes.txt", "README.md", "src/main.go"}},
	{name: "wc-lc-multi", tool: "wc", args: []string{"-lc", "notes.txt", "data.json"}},
	{name: "wc-w", tool: "wc", args: []string{"-w", "README.md", "notes.txt"}},
	{name: "wc-stdin", tool: "wc", stdin: "one two\nthree\n"},
	{name: "wc-stdin-l", tool: "wc", args: []string{"-l"}, stdin: "one two\nthree\nfour"},
	{name: "wc-stdin-and-file", tool: "wc", args: []string{"-l", "-", "notes.txt"}, stdin: "x\n"},
	{name: "wc-m-utf8", tool: "wc", args: []string{"-mwlc", "unicode.txt"}, locale: "C.UTF-8"},
	{name: "wc-missing", tool: "wc", args: []string{"notes.txt", "nope.txt"}},
	{name: "wc-empty", tool: "wc", stdin: ""},

	// jq
	{name: "jq-identity", tool: "jq", args: []string{".", "data.json"}},
	{name: "jq-compact", tool: "jq", args: []string{"-c", ".", "data.json"}},
	{name: "jq-path", tool: "jq", args: []string{".users[0].name", "data.json"}},
	{name: "jq-neg-index", tool: "jq", args: []string{".users[-1].tags", "data.json"}},
	{name: "jq-select", tool: "jq", args: []string{"-r", ".users[] | select(.age > 30) | .name", "data.json"}},
	{name: "jq-select-and", tool: "jq", args: []string{"-c", `.users[] | select(.age < 40 and (.tags | length) > 0) | {name, city}`, "data.json"}},
	{name: "jq-map", tool: "jq", args: []string{"-c", ".users | map(.age)", "data.json"}},
	{name: "jq-map-obj", tool: "jq", args: []string{`[.users[] | {who: .name, adult: (.age >= 18)}]`, "data.json"}},
	{name: "jq-keys", tool: "jq", args: []string{"-c", ".meta | keys, keys_unsorted", "data.json"}},
	{name: "jq-keys-array", tool: "jq", args: []string{"-c", ".users | keys", "data.json"}},
	{name: "jq-length", tool: "jq", args: []string{".users | length, (.weird | length), (.meta | length), (.empty | length)", "data.json"}},
	{name: "jq-numbers", tool: "jq", args: []string{"-c", ".meta | [.version, .count, .ratio, .big, .pi]", "data.json"}},
	{name: "jq-arith", tool: "jq", args: []string{"-c", "[(.users | map(.age) | add / length), .meta.count * 2 + 1, 10 / 4, .meta.ratio + 0]", "data.json"}},
	{name: "jq-strings", tool: "jq", args: []string{".weird", "data.json"}},
	{name: "jq-raw-strings", tool: "jq", args: []string{"-r", ".users[].city", "data.json"}},
	{name: "jq-missing", tool: "jq", args: []string{".nothing.deeper, .users[10]", "data.json"}},
	{name: "jq-optional", tool: "jq", args: []string{"-c", "[.users[].name[]?]", "data.json"}},
	{name: "jq-iterate-object", tool: "jq", args: []string{"-c", ".meta.nested[]", "data.json"}},
	{name: "jq-to-entries", tool: "jq", args: []string{"-c", ".meta.nested | to_entries", "data.json"}},
	{name: "jq-has-type", tool: "jq", args: []string{"-c", ".users | map(has(\"email\")), map(.email | type)", "data.json"}},
	{name: "jq-sort", tool: "jq", args: []string{"-c", "[.users[].city] | sort, (. | reverse)", "data.json"}},
	{name: "jq-alt", tool: "jq", args: []string{"-c", "[.users[] | .email // \"none\"]", "data.json"}},
	{name: "jq-stream", tool: "jq", args: []string{"-c", ". * 2, {v: .}"}, stdin: "1 2\n3"},
	{name: "jq-null-input", tool: "jq", args: []string{"-n", "-c", "[1, \"a\", null, true] | map(type)"}},
	{name: "jq-recurse", tool: "jq", args: []string{"-c", "[.. | select(type == \"number\")]"}, stdin: `{"a":[1,{"b":2}]}`},
	{name: "jq-error", tool: "jq", args: []string{".users.name", "data.json"}},
	{name: "jq-empty-containers", tool: "jq", args: []string{"{empty, list}", "data.json"}},

	// tree
	{name: "tree-default", tool: "tree", locale: "C.UTF-8"},
	{name: "tree-all", tool: "tree", args: []string{"-a"}, locale: "C.UTF-8"},
	{name: "tree-dirs", tool: "tree", args: []string{"-d"}, locale: "C.UTF-8"},
	{name: "tree-level", tool: "tree", args: []string{"-L", "1"}, locale: "C.UTF-8"},
	{name: "tree-gitignore", tool: "tree", args: []string{"--gitignore"}, locale: "C.UTF-8"},
	{name: "tree-gitignore-all", tool: "tree", args: []string{"-a", "--gitignore"}, locale: "C.UTF-8"},
	{name: "tree-subdir", tool: "tree", args: []string{"src", "docs"}, locale: "C.UTF-8"},
	{name: "tree-noreport", tool: "tree", args: []string{"--noreport", "src"}, locale: "C.UTF-8"},
}

func runMine(c tcase, dir string) (string, int) {
	var out, errb bytes.Buffer
	env := cli.Env{Stdin: strings.NewReader(c.stdin), Stdout: &out, Stderr: &errb, Dir: dir}
	var code int
	switch c.tool {
	case "grep":
		code = grep.Main(c.args, env)
	case "wc":
		code = wc.Main(c.args, env)
	case "jq":
		code = jqlite.Main(c.args, env)
	case "tree":
		code = tree.Main(c.args, env)
	}
	return out.String(), code
}

func realBinary(tool string) string {
	if env := os.Getenv(strings.ToUpper(tool) + "_BIN"); env != "" {
		return env
	}
	if p, err := exec.LookPath(tool); err == nil {
		return p
	}
	return ""
}

func runReal(t *testing.T, bin string, c tcase, dir string) (string, int) {
	cmd := exec.Command(bin, c.args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(c.stdin)
	locale := c.locale
	if locale == "" {
		locale = "C"
	}
	cmd.Env = append(os.Environ(), "LC_ALL="+locale, "GREP_COLORS=", "GREP_COLOR=", "NO_COLOR=1")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running %s: %v", bin, err)
	}
	return out.String(), code
}

func normalize(c tcase, s string) string {
	if !c.sortLines {
		return s
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func TestAgainstRealTools(t *testing.T) {
	dir := makeFixture(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mine, code := runMine(c, dir)
			got := normalize(c, mine) + fmt.Sprintf("[exit %d]\n", code)
			golden := filepath.Join("testdata", "golden", c.name+".txt")

			if bin := realBinary(c.tool); bin != "" {
				realOut, realCode := runReal(t, bin, c, dir)
				want := normalize(c, realOut) + fmt.Sprintf("[exit %d]\n", realCode)
				if *update {
					os.MkdirAll(filepath.Dir(golden), 0o755)
					if err := os.WriteFile(golden, []byte(want), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if got != want {
					t.Fatalf("differs from real %s:\n--- real\n%s--- mine\n%s", c.tool, want, got)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("no golden file and no real %s to compare against: %v", c.tool, err)
			}
			if got != string(want) {
				t.Fatalf("differs from golden %s:\n--- golden\n%s--- mine\n%s", golden, want, got)
			}
		})
	}
}
