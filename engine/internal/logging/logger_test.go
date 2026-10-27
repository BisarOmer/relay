package logging

import "testing"

func TestRedact(t *testing.T) {
	in := `password=supersecret token=abc Authorization: Bearer xyz`
	out := Redact(in)
	if contains(out, "supersecret") || contains(out, "abc") {
		t.Fatalf("secrets not redacted: %s", out)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && (s == sub || len(s) >= len(sub) && search(s, sub))
}

func search(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
