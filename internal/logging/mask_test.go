package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"url", true},
		{"URL", true},
		{"indexer_url", true},
		{"source_url", true},
		{"api_key", true},
		{"apikey", true},
		{"APIKey", true},
		{"cookie", true},
		{"Cookie", true},
		{"token", true},
		{"secret", true},
		{"password", true},
		{"passkey", true},
		{"authorization", true},
		{"title", false},
		{"seeders", false},
		{"id", false},
		{"uploader", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := isSensitiveKey(tt.key); got != tt.want {
				t.Fatalf("isSensitiveKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestMaskText(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantMasked  []string // substrings that must NOT survive
		wantPresent []string // substrings that must survive
	}{
		{
			name:       "embedded url with api key query param",
			in:         "probing https://real-indexer.example/search?apikey=SECRET123 for results",
			wantMasked: []string{"SECRET123", "real-indexer.example"},
		},
		{
			name:       "raw cookie header",
			in:         "sending Cookie: session=abc123XYZ",
			wantMasked: []string{"abc123XYZ"},
		},
		{
			name:       "raw api key assignment",
			in:         "using api_key=sk-live-999 for auth",
			wantMasked: []string{"sk-live-999"},
		},
		{
			name:        "ordinary message untouched",
			in:          "search dispatched to 3 sources",
			wantPresent: []string{"search dispatched to 3 sources"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskText(tt.in)

			for _, s := range tt.wantMasked {
				if strings.Contains(got, s) {
					t.Fatalf("maskText(%q) = %q, want %q redacted", tt.in, got, s)
				}
			}

			for _, s := range tt.wantPresent {
				if !strings.Contains(got, s) {
					t.Fatalf("maskText(%q) = %q, want %q preserved", tt.in, got, s)
				}
			}
		})
	}
}

// TestMaskingHandlerNestedGroups drives a real slog.Logger through the
// masking handler into a JSON sink and parses the output, so the assertion
// is against what would actually land in the log file — including a value
// nested two levels deep inside slog.Group attrs, which is what T-003
// requires beyond simple top-level masking.
func TestMaskingHandlerNestedGroups(t *testing.T) {
	var buf bytes.Buffer
	handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(handler)

	logger.Info("search dispatched",
		slog.String("indexer_url", "https://real-indexer.example/api?apikey=TOPSECRET"),
		slog.Group("request",
			slog.String("cookie", "session=zzz999"),
			slog.String("api_key", "sk-live-nested"),
			slog.Group("inner",
				slog.String("token", "deeply-nested-token"),
				slog.String("note", "not sensitive"),
			),
		),
	)

	line := buf.String()

	for _, secret := range []string{"TOPSECRET", "zzz999", "sk-live-nested", "deeply-nested-token"} {
		if strings.Contains(line, secret) {
			t.Fatalf("log line leaked secret %q: %s", secret, line)
		}
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode log line as JSON: %v (line: %s)", err, line)
	}

	if decoded["indexer_url"] != redacted {
		t.Fatalf("indexer_url = %v, want %q", decoded["indexer_url"], redacted)
	}

	request, ok := decoded["request"].(map[string]any)
	if !ok {
		t.Fatalf("request attr missing or not an object: %v", decoded["request"])
	}

	if request["cookie"] != redacted {
		t.Fatalf("request.cookie = %v, want %q", request["cookie"], redacted)
	}

	if request["api_key"] != redacted {
		t.Fatalf("request.api_key = %v, want %q", request["api_key"], redacted)
	}

	inner, ok := request["inner"].(map[string]any)
	if !ok {
		t.Fatalf("request.inner attr missing or not an object: %v", request["inner"])
	}

	if inner["token"] != redacted {
		t.Fatalf("request.inner.token = %v, want %q", inner["token"], redacted)
	}

	if inner["note"] != "not sensitive" {
		t.Fatalf("request.inner.note = %v, want it left alone", inner["note"])
	}
}

// TestMaskingHandlerWithAttrs confirms attributes bound via Logger.With are
// masked exactly like attributes passed at the call site — a derived
// logger is a common way an indexer adapter would carry its own URL/key
// around for every subsequent log call.
func TestMaskingHandlerWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(handler).With(
		slog.String("source_url", "https://real-indexer.example/feed?apikey=BOUND-SECRET"),
	)

	logger.Info("fan-out started")

	line := buf.String()
	if strings.Contains(line, "BOUND-SECRET") {
		t.Fatalf("log line leaked secret bound via With: %s", line)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode log line as JSON: %v (line: %s)", err, line)
	}

	if decoded["source_url"] != redacted {
		t.Fatalf("source_url = %v, want %q", decoded["source_url"], redacted)
	}
}

// TestMaskingHandlerMasksMessageText confirms a secret concatenated
// straight into the free-text message — not passed as a structured attr at
// all — still gets scrubbed, since AGENT.md requires masking "in every log
// line" rather than only in attributes an adapter remembered to name
// correctly.
func TestMaskingHandlerMasksMessageText(t *testing.T) {
	var buf bytes.Buffer
	handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(handler)

	logger.Warn("retrying against https://real-indexer.example/search?apikey=MSG-SECRET after timeout")

	line := buf.String()
	if strings.Contains(line, "MSG-SECRET") || strings.Contains(line, "real-indexer.example") {
		t.Fatalf("log line leaked secret/url embedded in message text: %s", line)
	}
}

func TestMaskingHandlerEnabledDelegates(t *testing.T) {
	inner := slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := newMaskingHandler(inner)

	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatalf("Enabled(Debug) = true, want false when inner handler is set to Warn")
	}

	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Fatalf("Enabled(Error) = false, want true when inner handler is set to Warn")
	}
}
