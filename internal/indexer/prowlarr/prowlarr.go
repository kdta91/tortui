// Package prowlarr talks to a self-hosted Prowlarr instance the user
// already runs, so its own configured indexers can be listed and imported
// as ordinary tortui sources (T-083).
//
// This is not an indexer.Indexer adapter: it never produces a
// indexer.Result and is never registered with the search registry. It is a
// one-shot listing client the settings screen's SourceManager seam uses to
// build an import wizard, the same way internal/indexer/torznab is an
// indexer.Indexer adapter for a Torznab feed. Nothing outside the settings
// screen's composition-root wiring may import this package directly
// (AGENT.md §4's registry-only rule is about search adapters; this package
// follows the same "only its one caller imports it" discipline for the same
// reason: internal/tui may never depend on a concrete implementation).
//
// Verified against Prowlarr's own source before implementing (T-083
// acceptance): GET /api/v1/indexer is Prowlarr's own documented indexer-list
// route (Prowlarr.Api.V1/Indexers/IndexerController.cs, inheriting
// ProviderControllerBase's GetAll), IndexerResource carries Id and Name
// (Prowlarr.Api.V1/Indexers/IndexerResource.cs), and every Servarr-family
// app — Prowlarr included — authenticates its own API with the X-Api-Key
// header (confirmed against Prowlarr's own AuthenticationBuilderExtensions.cs
// and its integration test client, NzbDrone.Integration.Test/Client/
// ClientBase.cs). The per-indexer Torznab feed URL an imported source is
// saved with, {base}/{indexerId}/api, is the same pattern Prowlarr's own
// frontend builds for its "copy RSS URL" button
// (frontend/src/Indexer/Index/Table/IndexerIndexRow.tsx).
package prowlarr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// headerAPIKey is the header every Servarr-family app (Sonarr, Radarr,
// Prowlarr, and the rest) checks for its own API key.
const headerAPIKey = "X-Api-Key"

// indexerListPath is Prowlarr's own documented indexer-list endpoint.
const indexerListPath = "/api/v1/indexer"

// Indexer is one entry from a Prowlarr instance's own configured indexer
// list. ID is Prowlarr's own numeric indexer id, stringified — it is what
// FeedURL needs to build that indexer's per-indexer Torznab feed URL, not a
// value tortui invents.
type Indexer struct {
	ID   string
	Name string
}

// indexerJSON is the subset of Prowlarr's IndexerResource this package
// reads. Every other field on that resource (protocol, capabilities,
// priority, and the rest) is Prowlarr's own configuration for a source it
// already knows how to reach; tortui only needs enough to offer the user a
// name to pick and an id to build a feed URL from.
type indexerJSON struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ParseError reports a response that did not parse as Prowlarr's own
// indexer-list JSON shape. It implements the duck-typed ParseFailed() shape
// internal/tui's settings screen matches via errors.As (T-081's taxonomy),
// without this package being imported there directly.
type ParseError struct {
	Cause error
}

// Error describes the failure without repeating any response content — the
// response could contain the user's own data from their own instance, but
// nothing this package has any business logging or displaying verbatim.
func (e *ParseError) Error() string {
	return fmt.Sprintf("prowlarr: parsing indexer list: %v", e.Cause)
}

// Unwrap exposes the underlying decode error.
func (e *ParseError) Unwrap() error { return e.Cause }

// ParseFailed always reports true: a *ParseError is only ever constructed
// for exactly that failure.
func (e *ParseError) ParseFailed() bool { return true }

// ListIndexers calls Prowlarr's GET /api/v1/indexer with the user's own
// API key for that instance (AGENT.md §2 — the only auth path this package
// ever uses) and returns every indexer the instance is configured with, in
// the order Prowlarr returned them.
//
// client is the shared httpx.Client every indexer adapter talks to the
// network through (T-020); a nil client gets a default one with no
// credentials of its own, since the API key here travels as a header this
// package sets directly rather than through httpx.Credentials (which only
// injects a query parameter or a Cookie header — neither is Prowlarr's own
// auth shape).
func ListIndexers(ctx context.Context, client *httpx.Client, baseURL, apiKey string) ([]Indexer, error) {
	if client == nil {
		client = httpx.New(httpx.Config{})
	}

	target := strings.TrimRight(strings.TrimSpace(baseURL), "/") + indexerListPath

	resp, err := client.Do(ctx, httpx.Request{
		Method: http.MethodGet,
		URL:    target,
		Header: http.Header{headerAPIKey: {apiKey}},
	})
	if err != nil {
		return nil, err
	}

	var raw []indexerJSON
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, &ParseError{Cause: err}
	}

	out := make([]Indexer, 0, len(raw))
	for _, r := range raw {
		out = append(out, Indexer{ID: strconv.Itoa(r.ID), Name: r.Name})
	}

	return out, nil
}

// FeedURL returns the per-indexer Torznab feed URL Prowlarr exposes for
// indexerID on the instance at baseURL — exactly the pattern Prowlarr's own
// frontend builds for its "copy RSS URL" action: {base}/{indexerId}/api.
// The caller saves this as an ordinary torznab source's URL, with the same
// API key as its credential (config.Indexer.APIKey) — nothing about the
// saved source is special-cased afterwards (T-083 acceptance).
func FeedURL(baseURL, indexerID string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/" + indexerID + "/api"
}
