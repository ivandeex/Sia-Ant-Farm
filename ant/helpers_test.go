package ant

import "testing"

// TestCapitalize tests capitalize function
func TestCapitalize(t *testing.T) {
	type subtest struct {
		name, s, exp string
	}
	tests := []subtest{
		{name: "TestEmptyString", s: "", exp: ""},
		{name: "TestOneLetter", s: "x", exp: "X"},
		{name: "TestDigits", s: "234", exp: "234"},
		{name: "TestWord", s: "hello", exp: "Hello"},
		{name: "TestWords", s: "hello to the World", exp: "Hello to the World"},
	}

	for _, st := range tests {
		t.Run(st.name, func(t *testing.T) {
			result := capitalize(st.s)
			if result != st.exp {
				t.Errorf("expected %v, got %v\n", st.exp, result)
			}
		})
	}
}
