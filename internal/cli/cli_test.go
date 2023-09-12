package cli

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	spec := Spec{Bool: "abc", Value: "e", Long: map[string]string{"all": "a", "level": "L"}, LongValue: map[string]bool{"level": true}}
	p, err := Parse(spec, []string{"-ab", "x", "-e", "pat", "--all", "-epat2", "--level=3", "y", "--", "-c"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Flags["a"] != 2 || p.Flags["b"] != 1 || p.Has("c") {
		t.Fatalf("flags %v", p.Flags)
	}
	if !reflect.DeepEqual(p.Values["e"], []string{"pat", "pat2"}) || p.Values["L"][0] != "3" {
		t.Fatalf("values %v", p.Values)
	}
	if !reflect.DeepEqual(p.Args, []string{"x", "y", "-c"}) {
		t.Fatalf("args %v", p.Args)
	}
	for _, bad := range [][]string{{"-z"}, {"-e"}, {"--nope"}, {"--all=1"}} {
		if _, err := Parse(spec, bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}
