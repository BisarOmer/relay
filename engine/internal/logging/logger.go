package logging

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|token|secret|authorization|nkey|seed)["']?\s*[:=]\s*["']?[^\s"',}]+`),
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9\-._~+/]+=*`),
}

var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/\s@]+@`)

// NKey seeds start with "S" followed by a type prefix and base32 data.
var nkeySeed = regexp.MustCompile(`\bS[UNOAC][A-Z2-7]{56}\b`)

// Redact replaces likely secrets in free-form text.
func Redact(s string) string {
	out := urlUserinfo.ReplaceAllString(s, "${1}[REDACTED]@")
	out = nkeySeed.ReplaceAllString(out, "[REDACTED]")
	for _, re := range secretPatterns {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			parts := strings.SplitN(m, "=", 2)
			if len(parts) == 2 {
				return parts[0] + "=[REDACTED]"
			}
			parts = strings.SplitN(m, ":", 2)
			if len(parts) == 2 {
				return parts[0] + ":[REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return out
}

func New(w io.Writer, level slog.Level) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Value.Kind() == slog.KindString {
				a.Value = slog.StringValue(Redact(a.Value.String()))
			}
			return a
		},
	})
	return slog.New(handler)
}

func MustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"marshal_failed"}`
	}
	return Redact(string(b))
}
