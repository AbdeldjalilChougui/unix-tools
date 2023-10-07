package jqlite

import (
	"strings"
	"testing"
)

func run(t *testing.T, filter, input string) string {
	t.Helper()
	prog, err := Parse(filter)
	if err != nil {
		t.Fatalf("%s: %v", filter, err)
	}
	var in any
	if input != "" {
		if in, err = NewDecoder(strings.NewReader(input)).Next(); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Eval(prog, in)
	if err != nil {
		return "error: " + err.Error()
	}
	var parts []string
	for _, v := range out {
		var sb strings.Builder
		Encode(&sb, v, 0, false)
		parts = append(parts, sb.String())
	}
	return strings.Join(parts, " ")
}

func TestEval(t *testing.T) {
	cases := []struct{ filter, input, want string }{
		{".a.b", `{"a":{"b":[1,2]}}`, `[1,2]`},
		{`.["a"]`, `{"a":1}`, `1`},
		{".[1:]", `[1]`, "PARSE"},
		{"[.[] | . * 10]", `[1,2]`, `[10,20]`},
		{".a, .b", `{"a":1,"b":2}`, `1 2`},
		{"{a, b: (.a + 1)}", `{"a":1}`, `{"a":1,"b":2}`},
		{"{(.k): .v}", `{"k":"x","v":true}`, `{"x":true}`},
		{"[.[] | select(. != null)]", `[1,null,2]`, `[1,2]`},
		{"1 + 2 * 3 - 4 / 2", ``, `5`},
		{"\"a\" + \"b\", [1] + [2], null + 1, {\"a\":1} + {\"b\":2}", ``, `"ab" [1,2] 1 {"a":1,"b":2}`},
		{"[1,2,3] - [2]", ``, `[1,3]`},
		{"[3,1,null,\"a\",[0],{}] | sort", ``, `[null,1,3,"a",[0],{}]`},
		{"length", `"héllo"`, `5`},
		{"not", `null`, `true`},
		{".x // 7", `{}`, `7`},
		{"[.[] | numbers]", `[1]`, "error: numbers/0 is not defined"},
		{".a", `[1]`, `error: Cannot index array with "a"`},
		{"[.[] | tostring]", `[1,"s",[2]]`, `["1","s","[2]"]`},
		{"-(1 + 2)", ``, `-3`},
	}
	for _, c := range cases {
		if c.want == "PARSE" {
			if _, err := Parse(c.filter); err == nil {
				t.Errorf("%s: expected a parse error", c.filter)
			}
			continue
		}
		if got := run(t, c.filter, c.input); got != c.want {
			t.Errorf("%s on %s: got %s, want %s", c.filter, c.input, got, c.want)
		}
	}
}

func TestNumberFormatting(t *testing.T) {
	lits := map[string]string{"1e2": "1E+2", "1.0": "1.0", "0.10": "0.10", "-0": "-0",
		"100": "100", "1.5e300": "1.5E+300", "1e-7": "1E-7", "0.000001": "0.000001", "12.5e1": "125"}
	for in, want := range lits {
		if got, _ := canonicalLiteral(in); got != want {
			t.Errorf("literal %s: got %s want %s", in, got, want)
		}
	}
	floats := map[float64]string{1e17: "1e+17", 12e15: "12000000000000000", 1e19: "1e+19", 1e-5: "1e-05",
		0.0001: "0.0001", 10.0 / 3: "3.3333333333333335", 123456789012345678: "123456789012345680", 3: "3"}
	for in, want := range floats {
		if got := formatFloat(in); got != want {
			t.Errorf("float %v: got %s want %s", in, got, want)
		}
	}
}
