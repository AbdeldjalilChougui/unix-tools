package jqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// JSON values are represented as:
//
//	nil, bool, Number, string, []any, *Object
//
// Objects keep their key order, because jq prints keys in input order.

// Number keeps the original literal so unchanged numbers print exactly as
// jq 1.7 prints them (it preserves literals through decNumber).
type Number struct {
	F   float64
	Lit string // "" for computed numbers
}

func num(f float64) Number { return Number{F: f} }

// Object is an insertion-ordered map.
type Object struct {
	Keys []string
	M    map[string]any
}

func NewObject() *Object { return &Object{M: map[string]any{}} }

func (o *Object) Set(k string, v any) {
	if _, ok := o.M[k]; !ok {
		o.Keys = append(o.Keys, k)
	}
	o.M[k] = v
}

func (o *Object) Clone() *Object {
	c := &Object{Keys: append([]string(nil), o.Keys...), M: make(map[string]any, len(o.M))}
	for k, v := range o.M {
		c.M[k] = v
	}
	return c
}

// Decoder reads a stream of JSON values (jq accepts several in a row).
type Decoder struct{ d *json.Decoder }

func NewDecoder(r io.Reader) *Decoder {
	d := json.NewDecoder(r)
	d.UseNumber()
	return &Decoder{d}
}

// Next returns the next value or io.EOF.
func (d *Decoder) Next() (any, error) {
	tok, err := d.d.Token()
	if err != nil {
		return nil, err
	}
	return d.value(tok)
}

func (d *Decoder) value(tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '[':
			arr := []any{}
			for d.d.More() {
				tk, err := d.d.Token()
				if err != nil {
					return nil, err
				}
				v, err := d.value(tk)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := d.d.Token(); err != nil { // ']'
				return nil, err
			}
			return arr, nil
		case '{':
			obj := NewObject()
			for d.d.More() {
				kt, err := d.d.Token()
				if err != nil {
					return nil, err
				}
				vt, err := d.d.Token()
				if err != nil {
					return nil, err
				}
				v, err := d.value(vt)
				if err != nil {
					return nil, err
				}
				obj.Set(kt.(string), v)
			}
			if _, err := d.d.Token(); err != nil { // '}'
				return nil, err
			}
			return obj, nil
		}
		return nil, fmt.Errorf("unexpected %v", t)
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, err
		}
		return Number{F: f, Lit: string(t)}, nil
	case string, bool, nil:
		return t, nil
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

// ---- type names, truthiness, ordering ----

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case *Object:
		return "object"
	}
	return "unknown"
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	}
	return true
}

func typeRank(v any) int {
	switch t := v.(type) {
	case nil:
		return 0
	case bool:
		if t {
			return 2
		}
		return 1
	case Number:
		return 3
	case string:
		return 4
	case []any:
		return 5
	case *Object:
		return 6
	}
	return 7
}

// compare implements jq's total order:
// null < false < true < numbers < strings < arrays < objects.
func compare(a, b any) int {
	ra, rb := typeRank(a), typeRank(b)
	if ra != rb {
		return cmpInt(ra, rb)
	}
	switch x := a.(type) {
	case Number:
		y := b.(Number)
		switch {
		case x.F < y.F:
			return -1
		case x.F > y.F:
			return 1
		}
		return 0
	case string:
		return strings.Compare(x, b.(string))
	case []any:
		y := b.([]any)
		for i := 0; i < len(x) && i < len(y); i++ {
			if c := compare(x[i], y[i]); c != 0 {
				return c
			}
		}
		return cmpInt(len(x), len(y))
	case *Object:
		y := b.(*Object)
		// Objects compare by their sorted key sets first, then values.
		kx, ky := sortedKeys(x), sortedKeys(y)
		if c := compare(strSlice(kx), strSlice(ky)); c != 0 {
			return c
		}
		for _, k := range kx {
			if c := compare(x.M[k], y.M[k]); c != 0 {
				return c
			}
		}
	}
	return 0
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func sortedKeys(o *Object) []string {
	k := append([]string(nil), o.Keys...)
	sort.Strings(k)
	return k
}

func strSlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// ---- output ----

// Encode writes v as JSON: pretty with 2-space indentation, or compact.
func Encode(sb *strings.Builder, v any, indent int, pretty bool) {
	nl := func(level int) {
		if pretty {
			sb.WriteByte('\n')
			sb.WriteString(strings.Repeat("  ", level))
		}
	}
	switch t := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		sb.WriteString(strconv.FormatBool(t))
	case Number:
		sb.WriteString(formatNumber(t))
	case string:
		writeString(sb, t)
	case []any:
		if len(t) == 0 {
			sb.WriteString("[]")
			return
		}
		sb.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			nl(indent + 1)
			Encode(sb, e, indent+1, pretty)
		}
		nl(indent)
		sb.WriteByte(']')
	case *Object:
		if len(t.Keys) == 0 {
			sb.WriteString("{}")
			return
		}
		sb.WriteByte('{')
		for i, k := range t.Keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			nl(indent + 1)
			writeString(sb, k)
			sb.WriteByte(':')
			if pretty {
				sb.WriteByte(' ')
			}
			Encode(sb, t.M[k], indent+1, pretty)
		}
		nl(indent)
		sb.WriteByte('}')
	}
}

func writeString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			sb.WriteString(`\"`)
		case r == '\\':
			sb.WriteString(`\\`)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\t':
			sb.WriteString(`\t`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r == '\b':
			sb.WriteString(`\b`)
		case r == '\f':
			sb.WriteString(`\f`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(sb, `\u%04x`, r)
		case r == utf8.RuneError && size == 1:
			sb.WriteString("�")
		default:
			sb.WriteString(s[i : i+size])
		}
		i += size
	}
	sb.WriteByte('"')
}

func formatNumber(n Number) string {
	if n.Lit != "" {
		if s, ok := canonicalLiteral(n.Lit); ok {
			return s
		}
	}
	return formatFloat(n.F)
}

// canonicalLiteral reproduces decNumber's to-scientific-string, which jq 1.7
// uses for numbers that came from the input unchanged: "1e2" → "1E+2",
// "1.0" → "1.0", "0.10" → "0.10".
func canonicalLiteral(lit string) (string, bool) {
	s := lit
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	mant, expStr, hasExp := strings.Cut(strings.ToLower(s), "e")
	intPart, frac, _ := strings.Cut(mant, ".")
	exp := 0
	if hasExp {
		e, err := strconv.Atoi(expStr)
		if err != nil {
			return "", false
		}
		exp = e
	}
	digits := strings.TrimLeft(intPart+frac, "0")
	if digits == "" {
		digits = "0"
	}
	exp -= len(frac)
	adjusted := exp + len(digits) - 1
	var out string
	switch {
	case exp <= 0 && adjusted >= -6:
		if exp == 0 {
			out = digits
		} else if point := len(digits) + exp; point > 0 {
			out = digits[:point] + "." + digits[point:]
		} else {
			out = "0." + strings.Repeat("0", -point) + digits
		}
	default:
		out = digits[:1]
		if len(digits) > 1 {
			out += "." + digits[1:]
		}
		sign := "+"
		if adjusted < 0 {
			sign = "-"
			adjusted = -adjusted
		}
		out += "E" + sign + strconv.Itoa(adjusted)
	}
	if neg {
		out = "-" + out
	}
	return out, true
}

// formatFloat prints a computed double the way jq 1.7 does: shortest
// round-trip digits, plain notation unless the exponent is far out.
func formatFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "null"
	case math.IsInf(f, 1):
		return "1.7976931348623157e+308"
	case math.IsInf(f, -1):
		return "-1.7976931348623157e+308"
	case f == 0:
		if math.Signbit(f) {
			return "-0"
		}
		return "0"
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	e := strconv.FormatFloat(f, 'e', -1, 64) // d.dddde±XX
	mant, expStr, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	x, _ := strconv.Atoi(expStr)
	decpt := x + 1
	nd := len(digits)
	switch {
	case decpt <= -4 || decpt > nd+15:
		out := digits[:1]
		if nd > 1 {
			out += "." + digits[1:]
		}
		es := "+"
		ex := decpt - 1
		if ex < 0 {
			es, ex = "-", -ex
		}
		return sign + out + "e" + es + fmt.Sprintf("%02d", ex)
	case decpt <= 0:
		return sign + "0." + strings.Repeat("0", -decpt) + digits
	case decpt >= nd:
		return sign + digits + strings.Repeat("0", decpt-nd)
	default:
		return sign + digits[:decpt] + "." + digits[decpt:]
	}
}
