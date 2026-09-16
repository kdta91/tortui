// Masking guarantees and limits (read this before trusting a log line):
//
// What IS caught, in every case tested so far, regardless of nesting depth,
// slog.Group, Logger.With, or slog.Any:
//   - Any attribute (or struct field, or map key) whose name is sensitive
//     per isSensitiveKey — a url/apikey/cookie/token/secret/password/
//     passkey/authorization-shaped key — has its value replaced wholesale,
//     however deeply it is nested and whatever Kind it is.
//   - A URL-shaped substring anywhere in a string value or the message
//     text, including a query string or embedded basic-auth credential
//     (https://user:pass@host/...), is redacted wholesale.
//   - A "key=value"/"key: value" credential pair inside free text.
//   - A handful of well-known bare secret-token shapes (Stripe-style
//     sk-/sk_live_/pk_live_ keys, GitHub ghp_/gho_/.../github_pat_ tokens,
//     AWS AKIA... access key IDs, GitLab glpat- tokens, Slack xox?- tokens,
//     and JWTs), even with no surrounding "key=" text and under an
//     unrelated key name.
//   - A struct, map, slice, pointer, error, or fmt.Stringer passed via
//     slog.Any — walked recursively field-by-field / element-by-element,
//     applying the same key-name and text-shape rules above.
//   - A value hidden behind slog.LogValuer is resolved before any of the
//     above runs, so a custom LogValue() implementation can't bypass
//     masking by indirection.
//
// What is NOT, and cannot in general be, caught:
//   - An opaque secret with none of the recognizable shapes above — no
//     "http(s)://", no "key=value", no known vendor prefix, no JWT
//     structure — logged as a bare value under a key name that isn't
//     itself flagged as sensitive. Since tortui's indexers are entirely
//     user-supplied (AGENT.md §2), a given indexer's api_key or cookie
//     value can be any opaque string its operator issued, with no fixed
//     shape to pattern-match against. Distinguishing such a string from an
//     ordinary opaque identifier (an info-hash, a random ID, part of a
//     torrent title) is not solvable by key-name or shape matching without
//     an unacceptable false-positive rate.
//
// The real defense for that residual case is procedural, not technical:
// code that logs a credential-carrying value MUST do so under a key name
// containing one of the sensitiveKeySubstrings below (or inside a struct
// field / map key named that way, which is now walked the same as a
// top-level attribute) so the key-based rule — the one guarantee that
// doesn't depend on guessing a secret's shape — actually applies. Adapter
// code added in later tasks (T-020 onward, which will log indexer.Result
// values carrying SourceURL/Extra) must follow that convention; a review
// that lets a credential reach a log call under an unrelated key name is a
// masking gap this package cannot close on its own. See DEC-026.
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
)

// redacted replaces any value logging identifies as sensitive.
const redacted = "[REDACTED]"

// maskAnyMaxDepth caps recursion when walking a KindAny value (struct,
// map, slice, or pointer chain) so a self-referential or pathologically
// deep structure can't hang or stack-overflow the logger. Nothing in this
// codebase's domain types nests anywhere near this deep; hitting it is
// itself a sign the caller handed the logger something it shouldn't have.
const maskAnyMaxDepth = 8

// sensitiveKeySubstrings are matched case-insensitively, with "_" and "-"
// stripped from both sides, against every attribute key logging sees —
// including keys nested inside slog.Group values, attributes bound via
// Logger.With, struct field names, and map keys reached through a KindAny
// value. A key that matches has its value replaced wholesale rather than
// partially redacted: an indexer URL can carry an API key or session
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
// attribute — an indexer URL with an embedded ?apikey=... query parameter,
// or embedded basic-auth credentials (https://user:pass@host/...), is
// exactly the shape this guards against. Matching \S+ up to the next
// whitespace redacts the whole URL rather than just the query string, so a
// credential in either position is caught the same way.
var urlPattern = regexp.MustCompile(`https?://\S+`)

// credentialPattern matches a "key: value" or "key=value" pair whose key
// names a credential, so a raw Cookie header or "api_key=..." string logged
// as free text (outside a structured attribute) still gets scrubbed.
var credentialPattern = regexp.MustCompile(`(?i)(api[_-]?key|cookie|token|secret|password|passkey|authorization)(\s*[:=]\s*)([^\s&"',;]+)`)

// bearerPattern matches an "Authorization: Bearer <token>"-style value even
// when written without the "Authorization" key at all (e.g. a raw header
// dump), redacting only the token and leaving the scheme word visible.
var bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._-]{8,}`)

// knownSecretPrefixPattern matches a finite list of publicly documented
// bare secret-token shapes — the kind a caller might log as a plain string
// value with no surrounding "key=" text and under a key name that gives no
// hint it's sensitive (e.g. slog.String("value", "sk-live-...")). This is
// deliberately narrow: it only fires on prefixes/structures that are
// vanishingly unlikely to appear in ordinary application data (a torrent
// title, an info-hash, a user-facing message), trading recall for a low
// false-positive rate. It does not, and cannot, cover an arbitrary
// indexer's own opaque api_key/cookie format — see the package doc above.
var knownSecretPrefixPattern = regexp.MustCompile(
	`\bsk-(?:live-|test-)?[A-Za-z0-9-]{6,}\b` + // Stripe-style secret keys ("sk-live-...")
		`|\b(?:sk|pk)_(?:live|test)_[A-Za-z0-9]{6,}\b` + // Stripe-style, underscore variant
		`|\bgh[pousr]_[A-Za-z0-9]{20,}\b` + // GitHub personal/OAuth/app tokens
		`|\bgithub_pat_[A-Za-z0-9_]{20,}\b` + // GitHub fine-grained PATs
		`|\bAKIA[0-9A-Z]{16}\b` + // AWS access key IDs
		`|\bglpat-[A-Za-z0-9_-]{20,}\b` + // GitLab personal access tokens
		`|\bxox[baprs]-[A-Za-z0-9-]{10,}\b` + // Slack tokens
		`|\bey[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`, // JWTs
)

// Redact applies the same free-text masking maskText uses on every log
// record — URLs (including embedded basic-auth and query-string
// credentials), "key=value"/"key: value" credential pairs, bearer tokens,
// and known bare secret-token shapes — to an arbitrary string a caller
// wants to display outside the logger entirely.
//
// `tortui doctor` (T-055) is the motivating caller: an indexer reachability
// check surfaces a raw net/http error, which can embed the request URL
// (and therefore any api_key/cookie the user configured as a query
// parameter) verbatim in its Error() text. Piping that error through
// Redact before printing it gives doctor's output the same masking
// guarantee — and the same documented limits — as the log file, without a
// second, drifting copy of the regexes in this file. See the package doc
// above for exactly what is and is not caught.
func Redact(s string) string {
	return maskText(s)
}

// maskText redacts URLs, credential-shaped substrings, bearer tokens, and
// known bare secret-token shapes inside free text (log messages and
// non-sensitive-keyed string attrs), independent of the key-based masking
// in maskAttr.
func maskText(s string) string {
	s = urlPattern.ReplaceAllString(s, redacted)
	// bearerPattern runs before credentialPattern: credentialPattern's
	// "authorization" alternative would otherwise stop at the first
	// whitespace after "Bearer" and leave the actual token that follows
	// it untouched.
	s = bearerPattern.ReplaceAllString(s, "Bearer "+redacted)
	s = credentialPattern.ReplaceAllString(s, "$1$2"+redacted)
	s = knownSecretPrefixPattern.ReplaceAllString(s, redacted)

	return s
}

// maskAny returns a redaction-safe copy of an arbitrary value carried by a
// slog.KindAny attribute — the Kind slog.Any produces for anything that
// isn't a plain string, group, or one of slog's other built-in kinds
// (int64, bool, time.Time, time.Duration, ...). Without this, a struct
// (or map, slice, pointer, error) logged via slog.Any bypassed every
// key- and text-based check in maskAttr entirely, because its Kind is
// KindAny and its fields were invisible to it — this was a confirmed leak
// on PR #3 QA (a struct with a URL field carrying ?apikey=... survived
// completely).
//
// error and fmt.Stringer values are reduced to their formatted text and
// run through maskText, since neither exposes fields to walk. Everything
// else is walked structurally: pointers are dereferenced, map keys and
// struct field names are checked with isSensitiveKey exactly like a
// top-level attribute key, slice/array elements are masked one by one, and
// a plain string leaf goes through maskText. Anything else (numbers,
// bools, channels, funcs) has nothing to mask and is returned unchanged.
func maskAny(v any, depth int) any {
	if v == nil {
		return nil
	}

	if depth > maskAnyMaxDepth {
		return "[MASKING: DEPTH LIMIT EXCEEDED]"
	}

	if err, ok := v.(error); ok {
		return maskText(err.Error())
	}

	if s, ok := v.(fmt.Stringer); ok {
		return maskText(s.String())
	}

	rv := reflect.ValueOf(v)

	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}

		return maskAny(rv.Elem().Interface(), depth+1)

	case reflect.String:
		return maskText(rv.String())

	case reflect.Map:
		out := make(map[string]any, rv.Len())

		iter := rv.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())

			if isSensitiveKey(key) {
				out[key] = redacted
				continue
			}

			out[key] = maskAny(iter.Value().Interface(), depth+1)
		}

		return out

	case reflect.Slice, reflect.Array:
		n := rv.Len()
		out := make([]any, n)

		for i := 0; i < n; i++ {
			out[i] = maskAny(rv.Index(i).Interface(), depth+1)
		}

		return out

	case reflect.Struct:
		t := rv.Type()
		out := make(map[string]any, t.NumField())

		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}

			if isSensitiveKey(field.Name) {
				out[field.Name] = redacted
				continue
			}

			out[field.Name] = maskAny(rv.Field(i).Interface(), depth+1)
		}

		return out

	default:
		// Numbers, bools, channels, funcs: nothing string-shaped to mask.
		return v
	}
}

// maskAttr returns a copy of a with its value redacted if its key is
// sensitive, recursing into slog.Group values so a credential nested
// several levels deep is masked exactly like a top-level one, and walking
// KindAny values (structs, maps, slices, pointers, errors, Stringers) the
// same way. A LogValuer is resolved first so a custom LogValue()
// implementation can't hide a sensitive value from any of the above. A
// non-sensitive string value is still scanned with maskText, so a secret
// embedded in an unrelated field's free text is caught too.
func maskAttr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()

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

	switch a.Value.Kind() {
	case slog.KindString:
		if s := a.Value.String(); maskText(s) != s {
			return slog.Attr{Key: a.Key, Value: slog.StringValue(maskText(s))}
		}
	case slog.KindAny:
		return slog.Attr{Key: a.Key, Value: slog.AnyValue(maskAny(a.Value.Any(), 0))}
	}

	return a
}

// maskingHandler wraps another slog.Handler and redacts sensitive values —
// indexer URLs, API keys, and cookies — from every record before it reaches
// the wrapped handler, including values nested inside slog.Group
// attributes, attributes attached via Logger.With, values inside a
// slog.Any-logged struct/map/slice, and secrets embedded directly in the
// message text (T-003). See the package doc above for exactly what this
// does and does not guarantee.
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
// groups and KindAny values) before delegating to the wrapped handler.
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
