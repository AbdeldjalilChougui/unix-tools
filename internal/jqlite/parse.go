package jqlite

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Grammar (lowest to highest precedence):
//
//	pipe    = comma { "|" comma }
//	comma   = or { "," or }
//	or      = and { "or" and }
//	and     = cmp { "and" cmp }
//	cmp     = add [ ("=="|"!="|"<"|"<="|">"|">=") add ]
//	add     = mul { ("+"|"-") mul }
//	mul     = post { ("*"|"/"|"%") post }
//	post    = primary { "." ident | "." string | "[" [pipe] "]" | "?" }
//	primary = "." [ident | string | "[" [pipe] "]"] | ".." | number | string
//	        | "true" | "false" | "null" | "(" pipe ")" | "[" [pipe] "]"
//	        | "{" [entry {"," entry}] "}" | ident [ "(" pipe {";" pipe} ")" ]
//	        | "-" post

type tokKind int

const (
	tEOF tokKind = iota
	tPunct
	tIdent
	tString
	tNumber
	tField // ".name"
)

type tok struct {
	kind tokKind
	s    string
	pos  int
}

func lex(src string) ([]tok, error) {
	var out []tok
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '"':
			j := i + 1
			var sb strings.Builder
			for {
				if j >= len(src) {
					return nil, fmt.Errorf("unterminated string at %d", i)
				}
				if src[j] == '"' {
					break
				}
				if src[j] == '\\' && j+1 < len(src) {
					j++
					switch src[j] {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					case 'r':
						sb.WriteByte('\r')
					case 'b':
						sb.WriteByte('\b')
					case 'f':
						sb.WriteByte('\f')
					case 'u':
						if j+4 >= len(src) {
							return nil, fmt.Errorf("bad \\u escape")
						}
						n, err := strconv.ParseUint(src[j+1:j+5], 16, 16)
						if err != nil {
							return nil, fmt.Errorf("bad \\u escape")
						}
						sb.WriteRune(rune(n))
						j += 4
					case '(':
						return nil, fmt.Errorf("string interpolation is not supported")
					default:
						sb.WriteByte(src[j])
					}
					j++
					continue
				}
				sb.WriteByte(src[j])
				j++
			}
			out = append(out, tok{tString, sb.String(), i})
			i = j + 1
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				j++
			}
			if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
				j++
				if j < len(src) && (src[j] == '+' || src[j] == '-') {
					j++
				}
				for j < len(src) && src[j] >= '0' && src[j] <= '9' {
					j++
				}
			}
			out = append(out, tok{tNumber, src[i:j], i})
			i = j
		case c == '.' && i+1 < len(src) && isIdentStart(src[i+1]):
			j := i + 1
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			out = append(out, tok{tField, src[i+1 : j], i})
			i = j
		case isIdentStart(c):
			j := i
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			out = append(out, tok{tIdent, src[i:j], i})
			i = j
		default:
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch two {
			case "==", "!=", "<=", ">=", "..", "//":
				out = append(out, tok{tPunct, two, i})
				i += 2
				continue
			}
			if strings.IndexByte(".|,()[]{}:;<>+-*/%?$", c) < 0 {
				return nil, fmt.Errorf("unexpected character %q at %d", c, i)
			}
			out = append(out, tok{tPunct, string(c), i})
			i++
		}
	}
	return append(out, tok{tEOF, "", len(src)}), nil
}

func isIdentStart(c byte) bool { return c == '_' || unicode.IsLetter(rune(c)) }
func isIdent(c byte) bool      { return isIdentStart(c) || c >= '0' && c <= '9' }

// ---- AST ----

type node interface{}

type (
	identity struct{}
	recurse  struct{}
	field    struct {
		target node
		name   string
	}
	index    struct{ target, idx node }
	iterate  struct{ target node }
	optional struct{ body node }
	literal  struct{ v any }
	pipe     struct{ l, r node }
	comma    struct{ l, r node }
	binop    struct {
		op   string
		l, r node
	}
	arrayCons struct{ body node } // body may be nil
	objCons   struct{ entries []objEntry }
	call      struct {
		name string
		args []node
	}
	neg struct{ body node }
)

type objEntry struct {
	key   node // evaluates to string(s)
	value node // nil means shorthand {a} → {a: .a}
}

type parser struct {
	toks []tok
	p    int
}

// Parse compiles a filter.
func Parse(src string) (node, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	ps := &parser{toks: toks}
	n, err := ps.pipe()
	if err != nil {
		return nil, err
	}
	if ps.peek().kind != tEOF {
		return nil, fmt.Errorf("unexpected %q at %d", ps.peek().s, ps.peek().pos)
	}
	return n, nil
}

func (ps *parser) peek() tok { return ps.toks[ps.p] }
func (ps *parser) next() tok { t := ps.toks[ps.p]; ps.p++; return t }
func (ps *parser) isPunct(s string) bool {
	t := ps.peek()
	return t.kind == tPunct && t.s == s
}
func (ps *parser) isWord(s string) bool {
	t := ps.peek()
	return t.kind == tIdent && t.s == s
}
func (ps *parser) expect(s string) error {
	if !ps.isPunct(s) {
		t := ps.peek()
		if t.kind == tEOF {
			return fmt.Errorf("expected %q at end of filter", s)
		}
		return fmt.Errorf("expected %q at %d, found %q", s, t.pos, t.s)
	}
	ps.p++
	return nil
}

func (ps *parser) pipe() (node, error) {
	l, err := ps.comma()
	if err != nil {
		return nil, err
	}
	for ps.isPunct("|") {
		ps.p++
		r, err := ps.comma()
		if err != nil {
			return nil, err
		}
		l = pipe{l, r}
	}
	return l, nil
}

func (ps *parser) comma() (node, error) {
	l, err := ps.alt()
	if err != nil {
		return nil, err
	}
	for ps.isPunct(",") {
		ps.p++
		r, err := ps.alt()
		if err != nil {
			return nil, err
		}
		l = comma{l, r}
	}
	return l, nil
}

func (ps *parser) alt() (node, error) {
	l, err := ps.or()
	if err != nil {
		return nil, err
	}
	for ps.isPunct("//") {
		ps.p++
		r, err := ps.or()
		if err != nil {
			return nil, err
		}
		l = binop{"//", l, r}
	}
	return l, nil
}

func (ps *parser) or() (node, error) {
	l, err := ps.and()
	if err != nil {
		return nil, err
	}
	for ps.isWord("or") {
		ps.p++
		r, err := ps.and()
		if err != nil {
			return nil, err
		}
		l = binop{"or", l, r}
	}
	return l, nil
}

func (ps *parser) and() (node, error) {
	l, err := ps.cmp()
	if err != nil {
		return nil, err
	}
	for ps.isWord("and") {
		ps.p++
		r, err := ps.cmp()
		if err != nil {
			return nil, err
		}
		l = binop{"and", l, r}
	}
	return l, nil
}

func (ps *parser) cmp() (node, error) {
	l, err := ps.add()
	if err != nil {
		return nil, err
	}
	for _, op := range []string{"==", "!=", "<=", ">=", "<", ">"} {
		if ps.isPunct(op) {
			ps.p++
			r, err := ps.add()
			if err != nil {
				return nil, err
			}
			return binop{op, l, r}, nil
		}
	}
	return l, nil
}

func (ps *parser) add() (node, error) {
	l, err := ps.mul()
	if err != nil {
		return nil, err
	}
	for ps.isPunct("+") || ps.isPunct("-") {
		op := ps.next().s
		r, err := ps.mul()
		if err != nil {
			return nil, err
		}
		l = binop{op, l, r}
	}
	return l, nil
}

func (ps *parser) mul() (node, error) {
	l, err := ps.postfix()
	if err != nil {
		return nil, err
	}
	for ps.isPunct("*") || ps.isPunct("/") || ps.isPunct("%") {
		op := ps.next().s
		r, err := ps.postfix()
		if err != nil {
			return nil, err
		}
		l = binop{op, l, r}
	}
	return l, nil
}

func (ps *parser) postfix() (node, error) {
	n, err := ps.primary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case ps.peek().kind == tField:
			n = field{n, ps.next().s}
		case ps.isPunct(".") && ps.toks[ps.p+1].kind == tString:
			ps.p++
			n = field{n, ps.next().s}
		case ps.isPunct(".") && ps.toks[ps.p+1].kind == tPunct && ps.toks[ps.p+1].s == "[":
			ps.p++ // ".[" after a term is the same as "["
		case ps.isPunct("["):
			ps.p++
			if ps.isPunct("]") {
				ps.p++
				n = iterate{n}
				continue
			}
			idx, err := ps.pipe()
			if err != nil {
				return nil, err
			}
			if err := ps.expect("]"); err != nil {
				return nil, err
			}
			n = index{n, idx}
		case ps.isPunct("?"):
			ps.p++
			n = optional{n}
		default:
			return n, nil
		}
	}
}

func (ps *parser) primary() (node, error) {
	t := ps.peek()
	switch t.kind {
	case tField:
		ps.p++
		return field{identity{}, t.s}, nil
	case tNumber:
		ps.p++
		f, err := strconv.ParseFloat(t.s, 64)
		if err != nil {
			return nil, fmt.Errorf("bad number %q", t.s)
		}
		return literal{Number{F: f, Lit: t.s}}, nil
	case tString:
		ps.p++
		return literal{t.s}, nil
	case tIdent:
		ps.p++
		switch t.s {
		case "true":
			return literal{true}, nil
		case "false":
			return literal{false}, nil
		case "null":
			return literal{nil}, nil
		}
		c := call{name: t.s}
		if ps.isPunct("(") {
			ps.p++
			for {
				a, err := ps.pipe()
				if err != nil {
					return nil, err
				}
				c.args = append(c.args, a)
				if ps.isPunct(";") {
					ps.p++
					continue
				}
				break
			}
			if err := ps.expect(")"); err != nil {
				return nil, err
			}
		}
		return c, nil
	case tEOF:
		return nil, fmt.Errorf("unexpected end of filter")
	}
	switch t.s {
	case ".":
		ps.p++
		if ps.peek().kind == tString {
			return field{identity{}, ps.next().s}, nil
		}
		return identity{}, nil
	case "..":
		ps.p++
		return recurse{}, nil
	case "(":
		ps.p++
		n, err := ps.pipe()
		if err != nil {
			return nil, err
		}
		return n, ps.expect(")")
	case "[":
		ps.p++
		if ps.isPunct("]") {
			ps.p++
			return arrayCons{}, nil
		}
		n, err := ps.pipe()
		if err != nil {
			return nil, err
		}
		return arrayCons{n}, ps.expect("]")
	case "{":
		ps.p++
		return ps.object()
	case "-":
		ps.p++
		n, err := ps.postfix()
		if err != nil {
			return nil, err
		}
		return neg{n}, nil
	}
	return nil, fmt.Errorf("unexpected %q at %d", t.s, t.pos)
}

func (ps *parser) object() (node, error) {
	o := objCons{}
	if ps.isPunct("}") {
		ps.p++
		return o, nil
	}
	for {
		var e objEntry
		t := ps.next()
		switch {
		case t.kind == tIdent || t.kind == tString:
			e.key = literal{t.s}
		case t.kind == tPunct && t.s == "(":
			k, err := ps.pipe()
			if err != nil {
				return nil, err
			}
			if err := ps.expect(")"); err != nil {
				return nil, err
			}
			e.key = k
		default:
			return nil, fmt.Errorf("unexpected %q in object at %d", t.s, t.pos)
		}
		if ps.isPunct(":") {
			ps.p++
			v, err := ps.objValue()
			if err != nil {
				return nil, err
			}
			e.value = v
		} else if _, ok := e.key.(literal); !ok {
			return nil, fmt.Errorf("computed object key needs a value")
		}
		o.entries = append(o.entries, e)
		if ps.isPunct(",") {
			ps.p++
			continue
		}
		return o, ps.expect("}")
	}
}

// Object values bind tighter than ",": {a: 1, b: 2}.
func (ps *parser) objValue() (node, error) {
	l, err := ps.alt()
	if err != nil {
		return nil, err
	}
	for ps.isPunct("|") {
		ps.p++
		r, err := ps.alt()
		if err != nil {
			return nil, err
		}
		l = pipe{l, r}
	}
	return l, nil
}
