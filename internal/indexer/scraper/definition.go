package scraper

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	yaml "github.com/goccy/go-yaml"
)

// Response modes a definition can declare.
const (
	// ModeHTML parses the response with goquery and reads CSS selectors.
	// It is the default, because it is what a site with a results page
	// serves.
	ModeHTML = "html"

	// ModeJSON parses the response as JSON and reads path expressions.
	ModeJSON = "json"
)

// Field names this schema maps onto indexer.Result. A definition that uses
// any other name is refused by validation, naming the offending key: a
// silently ignored field looks exactly like a rotted selector, and the
// point of the schema is that a user can tell those apart.
const (
	fieldID        = "id"
	fieldTitle     = "title"
	fieldInfoHash  = "infohash"
	fieldMagnet    = "magnet"
	fieldTorrent   = "torrent_url"
	fieldSize      = "size"
	fieldSeeders   = "seeders"
	fieldLeechers  = "leechers"
	fieldCategory  = "category"
	fieldPublished = "published"
	fieldUploader  = "uploader"
	fieldSource    = "source_url"
)

// fieldNames is every key allowed under a fields block, in the order a
// validation message lists them.
var fieldNames = []string{
	fieldID,
	fieldTitle,
	fieldInfoHash,
	fieldMagnet,
	fieldTorrent,
	fieldSize,
	fieldSeeders,
	fieldLeechers,
	fieldCategory,
	fieldPublished,
	fieldUploader,
	fieldSource,
}

// maxTransforms bounds one field's transform chain.
//
// The chain is a list applied once, in order, by a for loop over a slice of
// pure functions: there is no way to express a cycle in it and no transform
// re-enters the chain. The bound is therefore not a loop guard but a work
// guard — each transform is linear in the value's length, so an
// unreasonably long chain would multiply the cost of every field of every
// row by its length. Eight is far past any real definition.
const maxTransforms = 8

// Definition is one scraper source, as the user wrote it in YAML. It is
// plain data: nothing in it is compiled or resolved until New builds an
// Adapter from it, and Parse validates it without keeping anything.
//
// A definition never contains a credential. An api key or a session cookie
// belongs in the user's own config, from where httpx injects it into every
// request (AGENT.md §2); the schema has no placeholder for one and
// validation refuses any placeholder it does not define, so there is no
// spelling of "put my api key here" that this schema accepts. See
// docs/indexer-definitions.md and DEC-073.
type Definition struct {
	// ID is the stable identifier for this source: the registry key, the
	// config key, and Result.IndexerID. Required.
	ID string `yaml:"id"`

	// Name is the human-readable name shown in the TUI. Defaults to ID.
	Name string `yaml:"name"`

	// BaseURL is the absolute http or https address every request is
	// resolved against, and the base every relative link on the page is
	// resolved against. Required.
	BaseURL string `yaml:"base_url"`

	// Mode is the response mode: ModeHTML (the default) or ModeJSON.
	Mode string `yaml:"mode"`

	// RequiresAuth declares that this source needs credentials the user
	// supplied from their own account. It seeds Caps.RequiresAuth and
	// nothing else — tortui never works around a source's access
	// controls (AGENT.md §2).
	RequiresAuth bool `yaml:"requires_auth"`

	// Rows is the default selector for one result row, shared by every
	// block that does not override it.
	Rows string `yaml:"rows"`

	// Fields are the default field selectors, shared by every block. A
	// block's own Fields override these key by key.
	Fields map[string]Field `yaml:"fields"`

	// Trust is the default trust mapping, shared by every block.
	Trust *Trust `yaml:"trust"`

	// Search is the keyword-search request. Required: a source that
	// cannot be searched has nothing to offer.
	Search *Block `yaml:"search"`

	// Latest is the recent-additions request, for a site whose
	// recent-additions page differs from its results page. A definition
	// that omits it reports Caps.Latest = false and is never sent a
	// ModeLatest query.
	Latest *Block `yaml:"latest"`
}

// Block is one request this source can serve — a keyword search or a
// recent-additions feed.
//
// Rows, Fields and Trust are inherited from the definition when the block
// leaves them out, which is the common case: most sites render the same
// row markup on both pages and only the path differs. Setting one on the
// block overrides the shared value; Fields override key by key, so a block
// can replace one selector and inherit the rest.
type Block struct {
	// Path is appended to the definition's base_url. It may be empty,
	// which requests the base_url itself, and it may contain
	// placeholders (see Params).
	Path string `yaml:"path"`

	// Params are the query parameters, as templates. A value may contain
	// the placeholders {{query}}, {{limit}} and {{offset}}; a parameter
	// whose template resolves to empty is not sent at all.
	Params map[string]string `yaml:"params"`

	// Rows overrides the definition's rows selector for this block.
	Rows string `yaml:"rows"`

	// Fields override the definition's fields for this block, key by key.
	Fields map[string]Field `yaml:"fields"`

	// Trust overrides the definition's trust mapping for this block.
	Trust *Trust `yaml:"trust"`
}

// Field is how one value is pulled out of one result row.
//
// The value is read, whitespace-trimmed, matched against Regex when one is
// set, and then passed through Transform in order. A selector that matches
// nothing yields the empty string, which becomes the zero value of the
// Result field it feeds — never an error. Only a definition that is
// *structurally* wrong (an uncompilable selector, an unknown transform) is
// an error, and that is reported once, at validation, rather than per row.
type Field struct {
	// Selector is a CSS selector in an html definition, or a path
	// expression in a json one. Empty means the row itself.
	Selector string `yaml:"selector"`

	// Attr reads an HTML attribute of the selected element instead of its
	// text. It is meaningless — and refused — in a json definition.
	Attr string `yaml:"attr"`

	// Text asks explicitly for the element's text. It is the default, so
	// setting it is documentation rather than behaviour; setting it
	// together with Attr is a validation error.
	Text bool `yaml:"text"`

	// Regex narrows the value: the first capture group of the first
	// match, or the whole match when the expression has no group. No
	// match yields the empty string. The syntax is Go's RE2, which has no
	// backreferences and no lookaround and cannot backtrack
	// catastrophically.
	Regex string `yaml:"regex"`

	// Transform is a chain of named transforms applied in order. See
	// transforms for the list.
	Transform []string `yaml:"transform"`

	// Layouts are Go time layouts tried in order on the published field,
	// before the built-in ones. They are refused on any other field,
	// where they would silently do nothing.
	Layouts []string `yaml:"layouts"`
}

// Trust maps what a row says about its uploader onto indexer.Trust.
//
// Trust is display metadata and nothing else (AGENT.md §2): it may be shown
// as a badge, sorted on and filtered on, and it never gates behaviour.
type Trust struct {
	// Selector, Attr, Regex and Transform read the badge value out of the
	// row exactly as a Field does.
	Spec Field `yaml:",inline"`

	// Values maps a read value onto a trust level: one of "none",
	// "verified", "trusted", "vip" or "unknown". Matching is
	// case-insensitive on the trimmed value. A value that is not in the
	// map, and a selector that matched nothing, both yield TrustUnknown —
	// "this source said nothing", which indexer.Trust documents as
	// different information from TrustNone.
	Values map[string]string `yaml:"values"`
}

// yamlPosition matches the "[line:column]" prefix goccy puts on every
// decode error.
var yamlPosition = regexp.MustCompile(`^\[(\d+):(\d+)\]`)

// Parse decodes a definition file and validates it.
//
// Decoding is strict: a key the schema does not define, a duplicate key,
// and a value of the wrong type are all errors rather than silently
// ignored. That is worth more than tolerance here — a mistyped key in a
// definition is indistinguishable from a selector that has rotted, and the
// user is editing this file precisely because something stopped working.
// It also bounds what a hostile definition can do: the anchors a YAML
// alias bomb needs somewhere to attach to, and an unknown top-level key is
// refused before anything is expanded (DEC-074).
//
// The error never contains any of the file's content. goccy's own message
// quotes the offending source line back, and a user is free to have
// written their own api key into a param value on that line, so only the
// line and column survive (DEC-072).
func Parse(data []byte) (*Definition, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("scraper: %w", ErrDefinitionEmpty)
	}

	var def Definition

	if err := yaml.UnmarshalWithOptions(data, &def, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("scraper: %w %s", ErrDefinitionMalformed, yamlErrorPlace(err))
	}

	if def.ID == "" && def.BaseURL == "" && def.Search == nil {
		// A document that decoded to nothing at all — "---", a bare
		// comment block, or a scalar — is more usefully reported as an
		// empty definition than as twelve missing keys.
		return nil, fmt.Errorf("scraper: %w", ErrDefinitionEmpty)
	}

	if err := def.Validate(); err != nil {
		return nil, err
	}

	return &def, nil
}

// yamlErrorPlace renders where a decode failed, and nothing about what was
// there. goccy's FormatError with inclSource=false still returns a message
// that can name a key, so only the "[line:column]" prefix it puts in front
// of that message is used.
func yamlErrorPlace(err error) string {
	plain := yaml.FormatError(err, false, false)

	if m := yamlPosition.FindStringSubmatch(plain); m != nil {
		return fmt.Sprintf("(at line %s, column %s)", m[1], m[2])
	}

	return "(the parser reported no position)"
}

// Validate reports every structural problem with a definition, one at a
// time, as a *ValidationError naming the failing key and — when the failure
// is about one — the selector.
//
// It compiles every selector and every regex to do it, and throws the
// result away; New compiles them again and keeps them. The duplication is
// deliberate: a caller that only wants to check a file (the settings
// screen's validate-before-save) should not have to build an adapter, and
// an exported Definition a caller filled in by hand should not be able to
// reach New without the same checks.
func (d *Definition) Validate() error {
	_, err := d.plan()

	return err
}

// mode returns the definition's response mode, defaulting to html.
func (d *Definition) mode() string {
	if strings.TrimSpace(d.Mode) == "" {
		return ModeHTML
	}

	return strings.ToLower(strings.TrimSpace(d.Mode))
}

// name returns the display name, defaulting to the id.
func (d *Definition) name() string {
	if n := strings.TrimSpace(d.Name); n != "" {
		return n
	}

	return strings.TrimSpace(d.ID)
}

// effectiveRows returns the rows selector in force for a block: the
// block's own, or the definition's when the block does not set one.
func (d *Definition) effectiveRows(b *Block) string {
	if r := strings.TrimSpace(b.Rows); r != "" {
		return r
	}

	return strings.TrimSpace(d.Rows)
}

// effectiveFields returns the field set in force for a block: the
// definition's fields, with the block's own overriding key by key.
//
// Overriding is per key rather than wholesale. A site whose recent-
// additions page differs in one column should be able to say so in three
// lines instead of restating every selector, and restating them is how a
// definition rots in one place and not the other.
func (d *Definition) effectiveFields(b *Block) map[string]Field {
	out := make(map[string]Field, len(d.Fields)+len(b.Fields))

	for name, f := range d.Fields {
		out[name] = f
	}

	for name, f := range b.Fields {
		out[name] = f
	}

	return out
}

// effectiveTrust returns the trust mapping in force for a block.
//
// Unlike fields, this is wholesale: a trust block is a selector plus the
// value map that goes with it, and merging half of one definition's map
// into another's selector would produce a mapping neither of them wrote.
func (d *Definition) effectiveTrust(b *Block) *Trust {
	if b.Trust != nil {
		return b.Trust
	}

	return d.Trust
}
