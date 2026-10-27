package security

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateEndpointURL checks that raw is an absolute URL with one of the
// allowed schemes and a host, and that it carries no embedded credentials
// (which would otherwise be persisted in plaintext and shown in the UI).
func ValidateEndpointURL(raw string, allowedSchemes ...string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q is not a valid URL", RedactURL(raw))
	}
	if u.User != nil {
		return fmt.Errorf("URL must not contain credentials; use the authentication settings instead")
	}
	scheme := strings.ToLower(u.Scheme)
	ok := false
	for _, s := range allowedSchemes {
		if scheme == s {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("unsupported URL scheme %q (expected %s)", u.Scheme, strings.Join(allowedSchemes, ", "))
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL %q has no host", raw)
	}
	if port := u.Port(); port != "" {
		var n int
		if _, err := fmt.Sscanf(port, "%d", &n); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("URL %q has an invalid port", raw)
		}
	}
	if strings.ContainsAny(host, " \t") {
		return fmt.Errorf("URL %q has an invalid host", raw)
	}
	return nil
}

// RedactURL replaces any password in a URL's userinfo with "redacted".
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, has := u.User.Password(); has {
		u.User = url.UserPassword(u.User.Username(), "redacted")
	} else {
		u.User = url.User("redacted")
	}
	return u.String()
}
