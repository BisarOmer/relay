package natsadapter

import "testing"

func TestValidateSubject(t *testing.T) {
	cases := []struct {
		subject string
		wild    bool
		ok      bool
	}{
		{"foo.bar", false, true},
		{"foo.*", true, true},
		{"foo.>", true, true},
		{"foo.>", false, false},
		{"*.bar", true, true},
		{">.bar", true, false},
		{"foo..bar", false, false},
		{"", false, false},
		{"foo bar", false, false},
	}
	for _, tc := range cases {
		err := ValidateSubject(tc.subject, tc.wild)
		if tc.ok && err != nil {
			t.Fatalf("%q wild=%v: unexpected err %v", tc.subject, tc.wild, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("%q wild=%v: expected error", tc.subject, tc.wild)
		}
	}
}
