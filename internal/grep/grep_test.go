package grep

import "testing"

func TestBREToRE2(t *testing.T) {
	for bre, want := range map[string]string{
		`a+b`:         `a\+b`,
		`a\+b`:        `a+b`,
		`\(ab\)*`:     `(ab)*`,
		`x\|y`:        `x|y`,
		`a{2}`:        `a\{2\}`,
		`a\{2\}`:      `a{2}`,
		`*star`:       `\*star`,
		`^*star`:      `^\*star`,
		`\(*x\)`:      `(\*x)`,
		`[(+)]`:       `[(+)]`,
		`[[:alpha:]]`: `[[:alpha:]]`,
		`[]a]`:        `[]a]`,
		`a.c\.`:       `a.c\.`,
	} {
		if got := BREToRE2(bre); got != want {
			t.Errorf("BREToRE2(%q) = %q, want %q", bre, got, want)
		}
	}
}
