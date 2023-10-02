// Package tree prints a directory tree like tree(1): -a (hidden files),
// -d (directories only), -L depth, --gitignore, --noreport.
package tree

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"

	"github.com/AbdeldjalilChougui/unix-tools/internal/cli"
	"github.com/AbdeldjalilChougui/unix-tools/internal/gitignore"
)

var spec = cli.Spec{
	Bool:  "ad",
	Value: "L",
	Long:  map[string]string{"gitignore": "gitignore", "noreport": "noreport"},
}

type opts struct {
	all, dirsOnly, useGitignore bool
	maxDepth                    int
	ign                         *gitignore.Matcher
	dirs, files                 int
}

// Main runs tree.
func Main(args []string, env cli.Env) int {
	p, err := cli.Parse(spec, args)
	if err != nil {
		fmt.Fprintf(env.Stderr, "tree: %v\n", err)
		return 2
	}
	o := &opts{all: p.Has("a"), dirsOnly: p.Has("d"), useGitignore: p.Has("gitignore")}
	if v, ok := p.Last("L"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fmt.Fprintln(env.Stderr, "tree: Invalid level, must be greater than 0.")
			return 2
		}
		o.maxDepth = n
	}
	roots := p.Args
	if len(roots) == 0 {
		roots = []string{"."}
	}
	out := bufio.NewWriter(env.Stdout)
	defer out.Flush()
	status := 0
	for _, root := range roots {
		real := env.Path(root)
		info, err := os.Stat(real)
		if err != nil || !info.IsDir() {
			fmt.Fprintf(out, "%s  [error opening dir]\n", root)
			status = 2
			continue
		}
		out.WriteString(root + "\n")
		o.dirs++ // tree 2.x counts the root itself
		o.ign = gitignore.New()
		o.walk(out, real, "", "", 1)
	}
	if !p.Has("noreport") {
		out.WriteByte('\n')
		if o.dirsOnly {
			fmt.Fprintf(out, "%s\n", plural(o.dirs, "directory", "directories"))
		} else {
			fmt.Fprintf(out, "%s, %s\n", plural(o.dirs, "directory", "directories"), plural(o.files, "file", "files"))
		}
	}
	return status
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// walk prints the children of real. rel is the path relative to the root
// (for .gitignore matching) and prefix is the indentation drawn so far.
func (o *opts) walk(out *bufio.Writer, real, rel, prefix string, depth int) {
	if o.useGitignore {
		o.ign.AddFile(rel, real)
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		return
	}
	type item struct {
		name  string
		isDir bool
	}
	var items []item
	for _, e := range entries {
		name := e.Name()
		if !o.all && name[0] == '.' {
			continue
		}
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(path.Join(real, name)); err == nil {
				isDir = st.IsDir()
			}
		}
		if o.dirsOnly && !isDir {
			continue
		}
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		if o.useGitignore && (name == ".git" || o.ign.Ignored(childRel, isDir)) {
			continue
		}
		items = append(items, item{name, isDir})
	}
	// tree's default sort is by name (strcoll); with LC_ALL=C that's bytes.
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	for i, it := range items {
		last := i == len(items)-1
		// tree 2.x pads the vertical bar with two no-break spaces.
		branch, indent := "├── ", "│\u00a0\u00a0 "
		if last {
			branch, indent = "└── ", "    "
		}
		out.WriteString(prefix + branch + it.name)
		full := path.Join(real, it.name)
		if fi, err := os.Lstat(full); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(full); err == nil {
				out.WriteString(" -> " + target)
			}
			out.WriteByte('\n')
			// tree does not descend into symlinked directories by default.
			if it.isDir {
				o.dirs++
			} else {
				o.files++
			}
			continue
		}
		out.WriteByte('\n')
		if it.isDir {
			o.dirs++
			if o.maxDepth == 0 || depth < o.maxDepth {
				childRel := it.name
				if rel != "" {
					childRel = rel + "/" + it.name
				}
				o.walk(out, full, childRel, prefix+indent, depth+1)
			}
		} else {
			o.files++
		}
	}
}
