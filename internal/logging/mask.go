package logging

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// redacted replaces any value logging identifies as sensitive.
const redacted = "[REDACTED]"

// sensitiveKeySubstrings are matched case-insensitively, with "_" and "-"
// stripped from both sides, against every attribute key logging sees —
// including keys nested inside slog.Group values and attributes bound via
// Logger.With. A key that matches has its value replaced wholesale rather
// than partially redacted: an indexer URL can carry an API key or session
// cookie as a query parameter, so a partial mask that only touched a
// "cookie" or "api_key" key would still leak the same secret sitting in a
// "url" key right next to it.
var sensitiveKeySubstrings = []string{
	"url",
	"apikey",
	"cookie",
	"token",
	"secret",
	"password",
	"passkey",
	"authorization",
}

// isSensitiveKey reports whether key names a value that must never reach
// the log verbatim — an indexer URL, API key, or cookie (T-003), plus a
// few closely related credential shapes.
func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")

	for _, sub := range sensitiveKeySubstrings {
		if strings.Contains(normalized, sub) {
			return true
		}
	}

	return false
}

// urlPattern matches an absolute http(s) URL so it can be redacted wholesale
// even when it appears in free-text log messages rather than a structured
// attribute — an indexer URL with an embedded ?apikey=... query parameter is
// exactly the shape this guards against.
var urlPattern = regexp.MustCompile(`https?://\S+`)

// credentialPattern matches a "key: value" or "key=value" pair whose key
// names a credential, so a raw Cookie header or "api_key=..." string logged
// as free text (outside a structured attribute) still gets scrubbed.
var credentialPattern = regexp.MustCompile(`(?i)(api[_-]?key|cookie|token|secret|password|passkey|authorization)(\s*[:=]\s*)([^\s&"',;]+)`)

// maskText redacts URLs and credential-shaped substrings inside free text
// (log messages and non-sensitive-keyed string attrs), independent of the
// key-based masking in maskAttr.
func maskText(s string) string {
	s = urlPattern.ReplaceAllString(s, redacted)
	s = credentialPattern.ReplaceAllString(s, "$1$2"+redacted)

	return s
}

// maskAttr returns a copy of a with its value redacted if its key is
// sensitive, recursing into slog.Group values so a credential nested
// several levels deep is masked exactly like a top-level one. A
// non-sensitive string value is still scanned with maskText, so a secret
// embedded in an unrelated field's free text is caught too.
func maskAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		masked := make([]slog.Attr, len(group))

		for i, ga := range group {
			masked[i] = maskAttr(ga)
		}

		return slog.Attr{Key: a.Key, Value: slog.GroupValue(masked...)}
	}

	if isSensitiveKey(a.Key) {
		return slog.Attr{Key: a.Key, Value: slog.StringValue(redacted)}
	}

	if a.Value.Kind() == slog.KindString {
		if s := a.Value.String(); maskText(s) != s {
			return slog.Attr{Key: a.Key, Value: slog.StringValue(maskText(s))}
		}
	}

	return a
}

// maskingHandler wraps another slog.Handler and redacts sensitive values —
// indexer URLs, API keys, and cookies — from every record before it reaches
// the wrapped handler, including values nested inside slog.Group
// attributes, attributes attached via Logger.With, and secrets embedded
// directly in the message text (T-003).
type maskingHandler struct {
	next slog.Handler
}

// newMaskingHandler wraps next so every record it receives has already had
// sensitive values redacted.
func newMaskingHandler(next slog.Handler) slog.Handler {
	return &maskingHandler{next: next}
}

// Enabled reports whether the wrapped handler would record a message at
// level; masking never changes level filtering.
func (h *maskingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle masks r's message and every attribute (recursively, through
// groups) before delegating to the wrapped handler.
func (h *maskingHandler) Handle(ctx context.Context, r slog.Record) error {
	masked := slog.NewRecord(r.Time, r.Level, maskText(r.Message), r.PC)

	r.Attrs(func(a slog.Attr) bool {
		masked.AddAttrs(maskAttr(a))

		return true
	})

	return h.next.Handle(ctx, masked)
}

// WithAttrs masks attrs before binding them to a derived handler, so a
// logger built with Logger.With carries the same guarantee as one
// attaching attributes at the call site.
func (h *maskingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	masked := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		masked[i] = maskAttr(a)
	}

	return &maskingHandler{next: h.next.WithAttrs(masked)}
}

// WithGroup passes the group through to the wrapped handler; group naming
// doesn't affect which keys are sensitive, so no masking decision happens
// here.
func (h *maskingHandler) WithGroup(name string) slog.Handler {
	return &maskingHandler{next: h.next.WithGroup(name)}
}
