# unix-tools

[![Go Reference](https://pkg.go.dev/badge/github.com/AbdeldjalilChougui/unix-tools.svg)](https://pkg.go.dev/github.com/AbdeldjalilChougui/unix-tools)

Small Go versions of four command-line tools, each its own binary:

| binary | does | flags |
|---|---|---|
| `cmd/grep` | GNU-grep-compatible search | `-i -v -n -r -c`, plus `-E -F -q -H -e PAT` |
| `cmd/wc` | line/word/char/byte counts | `-l -w -m -c` |
| `cmd/jqlite` | a subset of jq | `-c -r -n` |
| `cmd/tree` | directory tree | `-a -d -L N --gitignore --noreport` |

The test suite runs each tool next to the real one (GNU grep 3.12, GNU wc
9.x, jq 1.7.1, tree 2.2.1) on the same fixture and requires byte-identical
stdout and the same exit status.

## What it teaches

Matching real tools exactly is mostly about the details you'd never notice:

- **grep.** POSIX *basic* regular expressions, where `+ ? | ( ) { }` are
  literal and `\+ \| \( \)` are the operators. They're translated to Go's
  RE2 syntax. It also covers filename prefixes with several files or `-r`,
  dropping `./` when `-r` has no operand, and exit codes 0/1/2.
- **wc.** GNU's column width is the number of digits in the *total size of
  the regular files*. It's at least 7 when any input is a pipe, and 1 when a
  single number is printed for a single input.
- **jqlite.** It's a recursive-descent parser for jq's grammar (pipes bind
  looser than `,`, which binds looser than `//`, `or`, `and`, comparisons and
  arithmetic) and a generator-style evaluator where each filter returns zero
  or more outputs. Objects keep their key order. Numbers print the way
  jq 1.7.1 prints them: input literals go through decNumber's canonical form
  (`1e2` → `1E+2`, `1.0` stays `1.0`), and computed doubles use shortest
  round-trip digits (`1e+17`, `3.3333333333333335`).
- **tree / gitignore.** Git's ignore rules: `**`, negation, directory-only
  patterns, anchoring by a slash, and nested `.gitignore` files overriding
  their parents. Also tree 2.x quirks: the root counts as a directory, and
  the vertical bar is padded with two no-break spaces.

## Install

Prebuilt binaries for Linux, macOS and Windows (amd64 and arm64) are on the
[releases page](https://github.com/AbdeldjalilChougui/unix-tools/releases/latest).
Each archive contains the `grep`, `jqlite`, `tree` and `wc` binaries, the README and the license; `checksums.txt`
has the SHA-256 sums. For example, on Linux amd64:

```sh
curl -LO https://github.com/AbdeldjalilChougui/unix-tools/releases/download/v0.1.0/unix-tools_0.1.0_linux_amd64.tar.gz
tar xzf unix-tools_0.1.0_linux_amd64.tar.gz
```

Or build from source with Go:

```sh
go install github.com/AbdeldjalilChougui/unix-tools/cmd/...@latest
```

Note that `grep`, `wc` and `tree` in `$(go env GOPATH)/bin` may shadow the system tools if that directory comes first on your `PATH`.

## Quick start

```sh
go build -o bin/ ./cmd/...
bin/grep -rn TODO .
printf 'a b\nc\n' | bin/wc
bin/jqlite -c '.items[] | select(.price > 10) | {name, price}' data.json
bin/tree --gitignore -L 2
```

### jqlite language

`.`, `.a.b`, `."key"`, `.[0]`, `.[-1]`, `.["k"]`, `.[]`, `..`, `?`, `|`, `,`,
`//`, `and`, `or`, `== != < <= > >=`, `+ - * / %`, unary `-`, literals,
`[...]` and `{a, b: .x, (.k): .v}` construction, and these builtins:
`select map keys keys_unsorted length has type not add sort reverse
tostring tonumber to_entries empty`.

## How the tests work

```sh
go test -race ./...
```

`compare_test.go` builds a fixture tree in a temp dir (code, notes, logs,
nested `.gitignore` files, UTF-8 text, a JSON document) and runs about 70
cases:

1. If the real tool is available (`grep`/`wc`/`jq`/`tree` on `PATH`, or
   `$JQ_BIN` / `$TREE_BIN`), the real output is the expectation.
2. In every case the output must also match `testdata/golden/<case>.txt`,
   recorded from the real tools. So the suite is still meaningful where jq or
   tree isn't installed.

Re-record the goldens with the real tools:

```sh
JQ_BIN=/path/to/jq-1.7.1 TREE_BIN=$PWD/scripts/tree-docker.sh go test . -update
```

`scripts/tree-docker.sh` runs tree 2.2.1 from a Debian container, for
machines without it. The tools run with `LC_ALL=C` (`C.UTF-8` for `wc -m`
and `tree`). The output of `grep -r` is sorted before comparing, because real
grep walks directories in filesystem order. There are also unit tests for the
BRE translator, the gitignore rules, the argument parser and jqlite's
evaluator and number formatting.

## Status and limitations

These are learning-sized subsets, not replacements.

- **grep:** no context options (`-A/-B/-C`), `-o`, `-l`, `-w`, `-x`,
  colour, or backreferences (RE2 has none). Character classes follow Go, so
  locale-specific collation differs. `-r` visits files in sorted order, not
  directory order. Binary detection is a simple NUL-byte check.
- **wc:** no `-L` or `--files0-from`. Word splitting uses Unicode white space
  plus no-break spaces, as GNU wc 9 does in a UTF-8 locale. In the C locale
  GNU's `-m` counts bytes, but this one always decodes UTF-8.
- **jqlite:** no variables (`as $x`), `reduce`/`foreach`, function
  definitions, string interpolation, slices (`.[1:]`), `path`/assignment
  operators, `--arg`, or `-S`. Results are fully materialised (no lazy
  streams). Error messages are close to jq's but not identical.
- **tree:** no `-I`, `-P`, sorting options, sizes, permissions or JSON
  output. Symlinked directories are listed but not followed. Only
  `.gitignore` files are read, not `.git/info/exclude` or the global
  excludes file.

## License

MIT. See [LICENSE](LICENSE).
