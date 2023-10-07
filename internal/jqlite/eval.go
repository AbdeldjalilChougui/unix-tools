package jqlite

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Every jq filter maps one input to zero or more outputs. eval returns them
// as a slice: simple, and fine for a "lite" implementation.

type jqError struct{ msg string }

func (e *jqError) Error() string { return e.msg }

func errorf(f string, a ...any) error { return &jqError{fmt.Sprintf(f, a...)} }

// errBreak-free "empty" is just an empty slice.

// Eval runs a compiled filter on one input.
func Eval(n node, in any) ([]any, error) { return eval(n, in) }

func eval(n node, in any) ([]any, error) {
	switch t := n.(type) {
	case identity:
		return []any{in}, nil
	case literal:
		return []any{t.v}, nil
	case recurse:
		var out []any
		var walk func(v any)
		walk = func(v any) {
			out = append(out, v)
			switch c := v.(type) {
			case []any:
				for _, e := range c {
					walk(e)
				}
			case *Object:
				for _, k := range c.Keys {
					walk(c.M[k])
				}
			}
		}
		walk(in)
		return out, nil
	case field:
		return each(t.target, in, func(v any) ([]any, error) {
			r, err := indexValue(v, t.name)
			if err != nil {
				return nil, err
			}
			return []any{r}, nil
		})
	case index:
		return each(t.target, in, func(v any) ([]any, error) {
			idxs, err := eval(t.idx, in) // the index is evaluated against the original input
			if err != nil {
				return nil, err
			}
			var out []any
			for _, i := range idxs {
				r, err := indexValue(v, i)
				if err != nil {
					return nil, err
				}
				out = append(out, r)
			}
			return out, nil
		})
	case iterate:
		return each(t.target, in, func(v any) ([]any, error) {
			switch c := v.(type) {
			case []any:
				return append([]any(nil), c...), nil
			case *Object:
				out := make([]any, 0, len(c.Keys))
				for _, k := range c.Keys {
					out = append(out, c.M[k])
				}
				return out, nil
			}
			return nil, errorf("Cannot iterate over %s", describe(v))
		})
	case optional:
		out, err := eval(t.body, in)
		var je *jqError
		if errors.As(err, &je) {
			return out, nil // "?" suppresses errors, keeping outputs so far
		}
		return out, err
	case pipe:
		left, err := eval(t.l, in)
		if err != nil {
			return nil, err
		}
		var out []any
		for _, v := range left {
			r, err := eval(t.r, v)
			out = append(out, r...) // keep outputs produced before an error
			if err != nil {
				return out, err
			}
		}
		return out, nil
	case comma:
		l, err := eval(t.l, in)
		if err != nil {
			return l, err
		}
		r, err := eval(t.r, in)
		return append(l, r...), err
	case neg:
		return each(t.body, in, func(v any) ([]any, error) {
			n, ok := v.(Number)
			if !ok {
				return nil, errorf("%s cannot be negated", describe(v))
			}
			return []any{num(-n.F)}, nil
		})
	case arrayCons:
		if t.body == nil {
			return []any{[]any{}}, nil
		}
		vs, err := eval(t.body, in)
		if err != nil {
			return nil, err
		}
		if vs == nil {
			vs = []any{}
		}
		return []any{vs}, nil
	case objCons:
		return evalObject(t, in)
	case binop:
		return evalBinop(t, in)
	case call:
		return evalCall(t, in)
	}
	return nil, fmt.Errorf("internal: unknown node %T", n)
}

// each evaluates target and applies fn to every output.
func each(target node, in any, fn func(any) ([]any, error)) ([]any, error) {
	vs, err := eval(target, in)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, v := range vs {
		r, err := fn(v)
		out = append(out, r...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func describe(v any) string {
	var sb strings.Builder
	Encode(&sb, v, 0, false)
	s := sb.String()
	if len(s) > 11 {
		s = s[:10] + "..."
	}
	return fmt.Sprintf("%s (%s)", typeName(v), s)
}

func indexValue(v any, idx any) (any, error) {
	switch c := v.(type) {
	case nil:
		switch idx.(type) {
		case string, Number, nil:
			return nil, nil
		}
	case *Object:
		if k, ok := idx.(string); ok {
			return c.M[k], nil
		}
	case []any:
		if n, ok := idx.(Number); ok {
			i := int(math.Floor(n.F))
			if i < 0 {
				i += len(c)
			}
			if i < 0 || i >= len(c) {
				return nil, nil
			}
			return c[i], nil
		}
	}
	switch k := idx.(type) {
	case string:
		return nil, errorf("Cannot index %s with %q", typeName(v), k)
	}
	return nil, errorf("Cannot index %s with %s", typeName(v), typeName(idx))
}

func evalObject(o objCons, in any) ([]any, error) {
	// Each entry can produce several keys/values; the result is the
	// cartesian product, as in jq.
	results := []*Object{NewObject()}
	for _, e := range o.entries {
		keys, err := eval(e.key, in)
		if err != nil {
			return nil, err
		}
		var next []*Object
		for _, kv := range keys {
			k, ok := kv.(string)
			if !ok {
				return nil, errorf("Object keys must be strings")
			}
			var vals []any
			if e.value == nil {
				v, err := indexValue(in, k)
				if err != nil {
					return nil, err
				}
				vals = []any{v}
			} else if vals, err = eval(e.value, in); err != nil {
				return nil, err
			}
			for _, base := range results {
				for _, v := range vals {
					c := base.Clone()
					c.Set(k, v)
					next = append(next, c)
				}
			}
		}
		results = next
	}
	out := make([]any, len(results))
	for i, r := range results {
		out[i] = r
	}
	return out, nil
}

func evalBinop(b binop, in any) ([]any, error) {
	switch b.op {
	case "and", "or":
		ls, err := eval(b.l, in)
		if err != nil {
			return nil, err
		}
		var out []any
		for _, l := range ls {
			if b.op == "and" && !truthy(l) {
				out = append(out, false)
				continue
			}
			if b.op == "or" && truthy(l) {
				out = append(out, true)
				continue
			}
			rs, err := eval(b.r, in)
			if err != nil {
				return nil, err
			}
			for _, r := range rs {
				out = append(out, truthy(r))
			}
		}
		return out, nil
	case "//":
		ls, err := eval(b.l, in)
		var je *jqError
		if err != nil && !errors.As(err, &je) {
			return nil, err
		}
		var out []any
		for _, l := range ls {
			if truthy(l) {
				out = append(out, l)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
		return eval(b.r, in)
	}
	// jq evaluates the right side first and loops over it in the outer loop.
	rs, err := eval(b.r, in)
	if err != nil {
		return nil, err
	}
	ls, err := eval(b.l, in)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, r := range rs {
		for _, l := range ls {
			v, err := arith(b.op, l, r)
			if err != nil {
				return out, err
			}
			out = append(out, v)
		}
	}
	return out, nil
}

func arith(op string, l, r any) (any, error) {
	switch op {
	case "==":
		return compare(l, r) == 0, nil
	case "!=":
		return compare(l, r) != 0, nil
	case "<":
		return compare(l, r) < 0, nil
	case "<=":
		return compare(l, r) <= 0, nil
	case ">":
		return compare(l, r) > 0, nil
	case ">=":
		return compare(l, r) >= 0, nil
	}
	ln, lok := l.(Number)
	rn, rok := r.(Number)
	if lok && rok {
		switch op {
		case "+":
			return num(ln.F + rn.F), nil
		case "-":
			return num(ln.F - rn.F), nil
		case "*":
			return num(ln.F * rn.F), nil
		case "/":
			if rn.F == 0 {
				return nil, errorf("%s and %s cannot be divided because the divisor is zero", describe(l), describe(r))
			}
			return num(ln.F / rn.F), nil
		case "%":
			a, b := int64(ln.F), int64(rn.F)
			if b == 0 {
				return nil, errorf("%s and %s cannot be divided because the divisor is zero", describe(l), describe(r))
			}
			if b < 0 {
				b = -b
			}
			return num(float64(a % b)), nil
		}
	}
	if op == "+" {
		switch {
		case l == nil:
			return r, nil
		case r == nil:
			return l, nil
		}
		switch x := l.(type) {
		case string:
			if y, ok := r.(string); ok {
				return x + y, nil
			}
		case []any:
			if y, ok := r.([]any); ok {
				return append(append([]any{}, x...), y...), nil
			}
		case *Object:
			if y, ok := r.(*Object); ok {
				c := x.Clone()
				for _, k := range y.Keys {
					c.Set(k, y.M[k])
				}
				return c, nil
			}
		}
	}
	if op == "-" {
		if x, ok := l.([]any); ok {
			if y, ok := r.([]any); ok {
				out := []any{}
				for _, e := range x {
					keep := true
					for _, f := range y {
						if compare(e, f) == 0 {
							keep = false
							break
						}
					}
					if keep {
						out = append(out, e)
					}
				}
				return out, nil
			}
		}
	}
	verb := map[string]string{"+": "added", "-": "subtracted", "*": "multiplied", "/": "divided", "%": "divided"}[op]
	return nil, errorf("%s and %s cannot be %s", describe(l), describe(r), verb)
}

func evalCall(c call, in any) ([]any, error) {
	arity := func(n int) error {
		if len(c.args) != n {
			return errorf("%s/%d is not defined", c.name, len(c.args))
		}
		return nil
	}
	one := func(v any) ([]any, error) { return []any{v}, nil }
	switch c.name {
	case "empty":
		return nil, arity(0)
	case "not":
		if err := arity(0); err != nil {
			return nil, err
		}
		return one(!truthy(in))
	case "length":
		if err := arity(0); err != nil {
			return nil, err
		}
		switch v := in.(type) {
		case nil:
			return one(num(0))
		case Number:
			return one(num(math.Abs(v.F)))
		case string:
			return one(num(float64(utf8.RuneCountInString(v))))
		case []any:
			return one(num(float64(len(v))))
		case *Object:
			return one(num(float64(len(v.Keys))))
		}
		return nil, errorf("%s has no length", describe(in))
	case "type":
		if err := arity(0); err != nil {
			return nil, err
		}
		return one(typeName(in))
	case "keys", "keys_unsorted":
		if err := arity(0); err != nil {
			return nil, err
		}
		switch v := in.(type) {
		case *Object:
			ks := v.Keys
			if c.name == "keys" {
				ks = sortedKeys(v)
			}
			return one(strSlice(ks))
		case []any:
			out := make([]any, len(v))
			for i := range v {
				out[i] = num(float64(i))
			}
			return one(out)
		}
		return nil, errorf("%s has no keys", describe(in))
	case "has":
		if err := arity(1); err != nil {
			return nil, err
		}
		return each(c.args[0], in, func(k any) ([]any, error) {
			switch v := in.(type) {
			case *Object:
				if s, ok := k.(string); ok {
					_, has := v.M[s]
					return one(has)
				}
			case []any:
				if n, ok := k.(Number); ok {
					return one(n.F >= 0 && int(n.F) < len(v))
				}
			}
			return nil, errorf("Cannot check whether %s has a %s key", typeName(in), typeName(k))
		})
	case "select":
		if err := arity(1); err != nil {
			return nil, err
		}
		conds, err := eval(c.args[0], in)
		if err != nil {
			return nil, err
		}
		var out []any
		for _, cond := range conds {
			if truthy(cond) {
				out = append(out, in)
			}
		}
		return out, nil
	case "map":
		if err := arity(1); err != nil {
			return nil, err
		}
		// map(f) is defined as [.[] | f].
		return eval(arrayCons{pipe{iterate{identity{}}, c.args[0]}}, in)
	case "add":
		if err := arity(0); err != nil {
			return nil, err
		}
		var items []any
		switch v := in.(type) {
		case []any:
			items = v
		case *Object:
			for _, k := range v.Keys {
				items = append(items, v.M[k])
			}
		case nil:
			return one(nil)
		default:
			return nil, errorf("Cannot iterate over %s", describe(in))
		}
		var acc any
		for _, it := range items {
			r, err := arith("+", acc, it)
			if err != nil {
				return nil, err
			}
			acc = r
		}
		return one(acc)
	case "sort":
		if err := arity(0); err != nil {
			return nil, err
		}
		arr, ok := in.([]any)
		if !ok {
			return nil, errorf("%s cannot be sorted, as it is not an array", describe(in))
		}
		s := append([]any(nil), arr...)
		sort.SliceStable(s, func(i, j int) bool { return compare(s[i], s[j]) < 0 })
		return one(s)
	case "reverse":
		if err := arity(0); err != nil {
			return nil, err
		}
		switch v := in.(type) {
		case []any:
			s := make([]any, len(v))
			for i, e := range v {
				s[len(v)-1-i] = e
			}
			return one(s)
		case nil:
			return one([]any{})
		}
		return nil, errorf("Cannot reverse %s", describe(in))
	case "tostring":
		if err := arity(0); err != nil {
			return nil, err
		}
		if s, ok := in.(string); ok {
			return one(s)
		}
		var sb strings.Builder
		Encode(&sb, in, 0, false)
		return one(sb.String())
	case "tonumber":
		if err := arity(0); err != nil {
			return nil, err
		}
		switch v := in.(type) {
		case Number:
			return one(v)
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return nil, errorf("Cannot parse %q as JSON", v)
			}
			return one(num(f))
		}
		return nil, errorf("%s cannot be parsed as a number", describe(in))
	case "to_entries":
		if err := arity(0); err != nil {
			return nil, err
		}
		o, ok := in.(*Object)
		if !ok {
			return nil, errorf("%s has no keys", describe(in))
		}
		out := make([]any, 0, len(o.Keys))
		for _, k := range o.Keys {
			e := NewObject()
			e.Set("key", k)
			e.Set("value", o.M[k])
			out = append(out, e)
		}
		return one(out)
	}
	return nil, errorf("%s/%d is not defined", c.name, len(c.args))
}
