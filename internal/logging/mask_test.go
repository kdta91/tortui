package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// --- PR #3 QA remediation: KindAny and bare-secret vectors below. ---

// reqInfo mirrors QA's exact repro shape: a struct with a URL field that
// carries a secret in its query string, logged via slog.Any so its value
// arrives as KindAny rather than KindString.
type reqInfo struct {
	URL   string
	Note  string
	Token string
}

// TestMaskAnyStructViaSlogAny is FINDING 2(b) from PR #3 QA: a struct
// logged via slog.Any was neither masked nor even text-scanned, because
// maskAttr previously only handled KindGroup and KindString.
func TestMaskAnyStructViaSlogAny(t *testing.T) {
	var buf bytes.Buffer
	handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(handler)

	logger.Info("dispatching", slog.Any("req", reqInfo{
		URL:   "https://real-indexer.example/search?apikey=STRUCT-SECRET",
		Note:  "not sensitive",
		Token: "also-a-secret-field",
	}))

	line := buf.String()
	for _, secret := range []string{"STRUCT-SECRET", "real-indexer.example", "also-a-secret-field"} {
		if strings.Contains(line, secret) {
			t.Fatalf("log line leaked secret %q from a slog.Any-logged struct: %s", secret, line)
		}
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode log line as JSON: %v (line: %s)", err, line)
	}

	req, ok := decoded["req"].(map[string]any)
	if !ok {
		t.Fatalf("req attr missing or not an object: %v", decoded["req"])
	}

	if req["URL"] != redacted {
		t.Fatalf("req.URL = %v, want %q", req["URL"], redacted)
	}

	if req["Token"] != redacted {
		t.Fatalf("req.Token = %v, want %q (sensitive field name)", req["Token"], redacted)
	}

	if req["Note"] != "not sensitive" {
		t.Fatalf("req.Note = %v, want it left alone", req["Note"])
	}
}

// TestMaskAnyMapStringString exercises the frozen indexer.Result.Extra
// shape (map[string]string) carried via slog.Any: a sensitive-named key
// inside the map must be redacted, and a non-sensitive key whose value is
// still URL-shaped must be text-scanned.
func TestMaskAnyMapStringString(t *testing.T) {
	var buf bytes.Buffer
	handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(handler)

	logger.Info("result extra", slog.Any("extra", map[string]string{
		"cookie": "session=MAP-SECRET",
		"origin": "https://real-indexer.example/details?apikey=MAP-URL-SECRET",
		"note":   "plain",
	}))

	line := buf.String()
	for _, secret := range []string{"MAP-SECRET", "MAP-URL-SECRET", "real-indexer.example"} {
		if strings.Contains(line, secret) {
			t.Fatalf("log line leaked secret %q from a slog.Any-logged map: %s", secret, line)
		}
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode log line as JSON: %v (line: %s)", err, line)
	}

	extra, ok := decoded["extra"].(map[string]any)
	if !ok {
		t.Fatalf("extra attr missing or not an object: %v", decoded["extra"])
	}

	if extra["cookie"] != redacted {
		t.Fatalf("extra.cookie = %v, want %q", extra["cookie"], redacted)
	}

	if extra["origin"] != redacted {
		t.Fatalf("extra.origin = %v, want %q (URL-shaped value)", extra["origin"], redacted)
	}

	if extra["note"] != "plain" {
		t.Fatalf("extra.note = %v, want it left alone", extra["note"])
	}
}

// TestMaskAnySliceAndPointer confirms a slice of structs and a pointer to a
// struct are both walked rather than passed through opaquely.
func TestMaskAnySliceAndPointer(t *testing.T) {
	var buf bytes.Buffer
	handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(handler)

	results := []reqInfo{
		{URL: "https://real-indexer.example/a?apikey=SLICE-SECRET-1"},
		{URL: "https://real-indexer.example/b?apikey=SLICE-SECRET-2"},
	}

	logger.Info("batch", slog.Any("results", results), slog.Any("single", &results[0]))

	line := buf.String()
	for _, secret := range []string{"SLICE-SECRET-1", "SLICE-SECRET-2"} {
		if strings.Contains(line, secret) {
			t.Fatalf("log line leaked secret %q from a slice/pointer via slog.Any: %s", secret, line)
		}
	}
}

// plainStringer is a custom fmt.Stringer (unlike time.Duration/time.Time,
// which slog gives their own native Kind and never routes through KindAny
// at all) so this test actually exercises maskAny's Stringer branch.
type plainStringer struct{ s string }

func (p plainStringer) String() string { return p.s }

// TestMaskAnyErrorAndStringer confirms error values and fmt.Stringer
// implementations logged via slog.Any are text-scanned via their
// formatted representation rather than passed through untouched.
func TestMaskAnyErrorAndStringer(t *testing.T) {
	var buf bytes.Buffer
	handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(handler)

	logger.Error("request failed",
		slog.Any("err", fmt.Errorf("GET https://real-indexer.example/search?apikey=ERR-SECRET: timeout")),
		slog.Any("note", plainStringer{s: "harmless status"}), // sanity: non-secret Stringer passes through
	)

	line := buf.String()
	if strings.Contains(line, "ERR-SECRET") || strings.Contains(line, "real-indexer.example") {
		t.Fatalf("log line leaked secret embedded in an error value: %s", line)
	}

	if !strings.Contains(line, "harmless status") {
		t.Fatalf("ordinary Stringer value was altered unexpectedly: %s", line)
	}
}

// TestMaskBareKnownSecretShapes is FINDING 2(a) from PR #3 QA: a secret
// logged as a bare string value under a key name that gives no hint it's
// sensitive (slog.String("value", "sk-live-...")). maskText's URL and
// key=value patterns don't match a bare token with neither shape, so this
// exercises the dedicated known-secret-prefix detection instead.
func TestMaskBareKnownSecretShapes(t *testing.T) {
	tests := []struct {
		name   string
		secret string
	}{
		{"stripe-style hyphenated", "sk-live-abcdef123456"},
		{"stripe-style underscored", "sk_live_abcdef123456"},
		{"github pat", "github_pat_11ABCDEFG0123456789abcdefghij0123456789"},
		{"github token", "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ012345"},
		{"aws access key", "AKIAABCDEFGHIJKLMNOP"},
		{"gitlab pat", "glpat-abcdefghijklmnopqrst"},
		{"slack token", "xoxb-1234567890-abcdefghij"},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
			logger := slog.New(handler)

			// "value" deliberately doesn't match isSensitiveKey.
			logger.Info("bare secret test", slog.String("value", tt.secret))

			line := buf.String()
			if strings.Contains(line, tt.secret) {
				t.Fatalf("log line leaked bare secret %q under a non-sensitive key: %s", tt.secret, line)
			}
		})
	}
}

// TestMaskTextBearerToken confirms an "Authorization: Bearer <token>"
// header dumped into free text has the token itself redacted, not just the
// "Authorization:" prefix — a naive ordering (credentialPattern before
// bearerPattern) stops at the first whitespace after "Bearer" and would
// leave the real token sitting right after it.
func TestMaskTextBearerToken(t *testing.T) {
	in := "sending header Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig-BEARER-SECRET"

	got := maskText(in)
	if strings.Contains(got, "BEARER-SECRET") {
		t.Fatalf("maskText(%q) = %q, want the bearer token redacted", in, got)
	}
}

// TestMaskTextBasicAuthURLUnderNonSensitiveKey confirms a URL with
// embedded basic-auth credentials is redacted wholesale even when it
// arrives as a plain string value under a key name that isn't itself
// flagged sensitive.
func TestMaskAttrBasicAuthURLUnderNonSensitiveKey(t *testing.T) {
	var buf bytes.Buffer
	handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(handler)

	logger.Info("probing", slog.String("detail", "https://user:BASICAUTH-SECRET@real-indexer.example/path"))

	line := buf.String()
	if strings.Contains(line, "BASICAUTH-SECRET") || strings.Contains(line, "real-indexer.example") {
		t.Fatalf("log line leaked basic-auth credentials under a non-sensitive key: %s", line)
	}
}

// TestMaskLogValuerResolved confirms a value hidden behind slog.LogValuer
// is resolved before masking runs, so a custom LogValue() implementation
// can't hide a sensitive value from either the key- or shape-based checks.
type secretLogValuer struct{ apiKey string }

func (s secretLogValuer) LogValue() slog.Value {
	return slog.StringValue(s.apiKey)
}

func TestMaskLogValuerResolved(t *testing.T) {
	var buf bytes.Buffer
	handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(handler)

	logger.Info("via logvaluer", slog.Any("api_key", secretLogValuer{apiKey: "LOGVALUER-SECRET"}))

	line := buf.String()
	if strings.Contains(line, "LOGVALUER-SECRET") {
		t.Fatalf("log line leaked secret hidden behind a LogValuer: %s", line)
	}
}

// TestMaskAnyDoesNotAlterOrdinaryStructs is a regression guard: a struct
// with no sensitive field names and no secret-shaped values must survive
// slog.Any unchanged in substance, so the new KindAny walk doesn't quietly
// degrade normal structured logging.
func TestMaskAnyDoesNotAlterOrdinaryStructs(t *testing.T) {
	var buf bytes.Buffer
	handler := newMaskingHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(handler)

	logger.Info("ordinary", slog.Any("info", reqInfo{URL: "", Note: "hello world", Token: ""}))

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode log line as JSON: %v (line: %s)", err, buf.String())
	}

	info, ok := decoded["info"].(map[string]any)
	if !ok {
		t.Fatalf("info attr missing or not an object: %v", decoded["info"])
	}

	if info["Note"] != "hello world" {
		t.Fatalf("info.Note = %v, want %q preserved", info["Note"], "hello world")
	}
}
