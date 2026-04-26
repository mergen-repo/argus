package dsl

import "testing"

func TestSuggest_FindsCloseMatch(t *testing.T) {
	got := Suggest("ap", []string{"apn", "operator", "sim"}, 2)
	if got != "apn" {
		t.Errorf("Suggest('ap') = %q, want 'apn'", got)
	}
}

func TestSuggest_FindsCloseMatch_TypoOneOff(t *testing.T) {
	got := Suggest("oprator", []string{"apn", "operator", "sim_type"}, 2)
	if got != "operator" {
		t.Errorf("Suggest('oprator') = %q, want 'operator'", got)
	}
}

func TestSuggest_NoMatchExceedsThreshold(t *testing.T) {
	got := Suggest("xyzabc", []string{"apn", "operator", "sim"}, 2)
	if got != "" {
		t.Errorf("Suggest('xyzabc') = %q, want empty string", got)
	}
}

func TestSuggest_EmptyInput(t *testing.T) {
	if got := Suggest("", []string{"apn"}, 2); got != "" {
		t.Errorf("Suggest('') = %q, want empty", got)
	}
}

func TestSuggest_EmptyCandidates(t *testing.T) {
	if got := Suggest("apn", nil, 2); got != "" {
		t.Errorf("Suggest with nil candidates = %q, want empty", got)
	}
}

func TestSuggest_CaseInsensitive(t *testing.T) {
	got := Suggest("APN", []string{"apn", "operator"}, 2)
	if got != "apn" {
		t.Errorf("Suggest('APN') = %q, want 'apn'", got)
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"apn", "ap", 1},
		{"oprator", "operator", 1},
		{"flaw", "lawn", 2},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
