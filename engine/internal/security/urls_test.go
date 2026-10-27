package security

import (
	"strings"
	"testing"
)

func TestValidateEndpointURL(t *testing.T) {
	ok := []string{"http://127.0.0.1:8222", "https://nats.example.com", "http://nats_1:8222", "http://[::1]:8222"}
	for _, u := range ok {
		if err := ValidateEndpointURL(u, "http", "https"); err != nil {
			t.Fatalf("%s: %v", u, err)
		}
	}
	bad := []string{"", "127.0.0.1:8222", "ftp://h", "http://", "http://u:p@h:8222", "http://h:99999", "file:///etc/passwd"}
	for _, u := range bad {
		err := ValidateEndpointURL(u, "http", "https")
		if err == nil {
			t.Fatalf("%q should be rejected", u)
		}
		if strings.Contains(err.Error(), ":p@") {
			t.Fatalf("error echoes credentials: %v", err)
		}
	}
}

func TestRedactURL(t *testing.T) {
	got := RedactURL("nats://alice:hunter2@h:4222")
	if strings.Contains(got, "hunter2") {
		t.Fatalf("not redacted: %s", got)
	}
	if RedactURL("nats://h:4222") != "nats://h:4222" {
		t.Fatal("plain URL altered")
	}
}
