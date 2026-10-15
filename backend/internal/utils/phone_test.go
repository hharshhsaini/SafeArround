package utils

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"9876543210":        "+919876543210",
		" 98765 43210 ":     "+919876543210",
		"+91 98765-43210":   "+919876543210",
		"919876543210":      "+919876543210",
		"(987) 654-3210":    "+919876543210",
		"+919876543210":     "+919876543210",
		"+1 (415) 555-0100": "+14155550100",
	}
	for input, want := range cases {
		if got := NormalizePhone(input); got != want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", input, got, want)
		}
	}
}
