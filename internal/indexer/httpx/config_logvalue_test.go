package httpx

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestConfigLogsAsReadableFieldsWithoutCredentials pins T-9168 (Backlog
// T-929): a Config logged whole renders its settings, not the JSON encoder's
// error for the func-typed Jitter field, and never a credential.
func TestConfigLogsAsReadableFieldsWithoutCredentials(t *testing.T) {
	t.Parallel()

	cfg := Config{
		UserAgent:                "tortui-test",
		RequestTimeout:           7 * time.Second,
		MaxAttempts:              2,
		Credentials:              testCredentials(),
		Transport:                roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil }),
		Jitter:                   func(d time.Duration) time.Duration { return d },
		FollowSubdomainRedirects: true,
	}

	for name, handler := range map[string]func(*bytes.Buffer) slog.Handler{
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
	} {
		var buf bytes.Buffer

		slog.New(handler(&buf)).Info("client configuration", "config", cfg)

		out := buf.String()
		if strings.Contains(out, "!ERROR") {
			t.Fatalf("%s: logging a Config produced an encoder error: %s", name, out)
		}

		for _, want := range []string{"tortui-test", "request_timeout", "jitter", "follow_subdomain_redirects", "[credentials redacted]"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: log line %q is missing %q", name, out, want)
			}
		}

		if strings.Contains(out, testAPIKey) || strings.Contains(out, "zzzz1111yyyy2222") {
			t.Fatalf("%s: log line carries a credential: %s", name, out)
		}
	}
}
