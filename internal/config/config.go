// Package config loads, validates, and writes tortui's TOML configuration
// file, and resolves the per-OS directories the application uses for
// configuration, state, and downloaded data (AGENT.md §14).
package config

// Config is the fully-populated application configuration, decoded from
// (and encoded to) config.toml. Every field has a documented default so a
// missing file, or a file that only sets a few keys, still produces a
// usable configuration (see Default and Load).
type Config struct {
	// DownloadDir is the default destination for downloaded torrents.
	// Per-torrent destinations (T-074) override this on a case-by-case
	// basis; this is only the fallback.
	DownloadDir string `toml:"download_dir"`

	// SavedDestinations lists additional destination roots the user has
	// chosen to remember, offered when adding a torrent (T-074).
	SavedDestinations []string `toml:"saved_destinations"`

	// MaxDownloadRate and MaxUploadRate cap engine throughput in bytes per
	// second. Zero means unlimited.
	MaxDownloadRate int64 `toml:"max_download_rate"`
	MaxUploadRate   int64 `toml:"max_upload_rate"`

	// MaxActiveDownloads bounds how many torrents download concurrently;
	// the rest queue (T-034).
	MaxActiveDownloads int `toml:"max_active_downloads"`

	// MaxPeers caps peer connections per torrent.
	MaxPeers int `toml:"max_peers"`

	// ListenPort is the BitTorrent listen port. Zero means "pick a free
	// port" (T-034).
	ListenPort int `toml:"listen_port"`

	// SeedPolicy is one of "ratio", "duration", or "off".
	SeedPolicy string `toml:"seed_policy"`

	// SeedRatio is the upload/download ratio to seed to when SeedPolicy is
	// "ratio".
	SeedRatio float64 `toml:"seed_ratio"`

	// MinFreeSpace is a human-readable size (e.g. "1GB", "500MB") below
	// which tortui refuses to start a new download (T-034). Parse with
	// ParseByteSize.
	MinFreeSpace string `toml:"min_free_space"`

	// SearchTimeout is a Go duration string (e.g. "15s") applied per
	// indexer during a search fan-out (T-012). Parse with
	// time.ParseDuration.
	SearchTimeout string `toml:"search_timeout"`

	// Theme selects a built-in TUI theme (T-050).
	Theme string `toml:"theme"`

	// ASCII forces the ASCII glyph fallback instead of Unicode block
	// characters, regardless of detected terminal capability.
	ASCII bool `toml:"ascii"`

	// Indexers is the user-configured source list. Bundled lawful sources
	// (T-024) are compiled into the binary separately and are never part
	// of this array.
	Indexers []Indexer `toml:"indexer"`
}

// Indexer is one user-configured search source.
type Indexer struct {
	// ID is a unique, stable identifier for this source, normally derived
	// by slugifying Name (T-080).
	ID string `toml:"id"`

	// Name is the human-readable label shown in the TUI.
	Name string `toml:"name"`

	// Type selects the adapter: "torznab" or "scraper".
	Type string `toml:"type"`

	// URL is the source's base or feed URL.
	URL string `toml:"url"`

	// APIKey and Cookie are credentials the user supplies from their own
	// account. tortui never discovers or harvests these (AGENT.md §2).
	APIKey string `toml:"api_key"`
	Cookie string `toml:"cookie"`

	// Definition names a scraper definition file (relative to the
	// definitions directory) when Type is "scraper".
	Definition string `toml:"definition"`

	// Enabled controls whether the registry queries this source.
	Enabled bool `toml:"enabled"`
}

// Default returns a fresh Config populated with tortui's built-in defaults,
// using downloadDir as the default download destination. Callers load a
// user's file on top of this (see Load) so that any key the user omits
// keeps its default value rather than a Go zero value.
func Default(downloadDir string) Config {
	return Config{
		DownloadDir:        downloadDir,
		SavedDestinations:  []string{},
		MaxDownloadRate:    0,
		MaxUploadRate:      0,
		MaxActiveDownloads: 3,
		MaxPeers:           50,
		ListenPort:         0,
		SeedPolicy:         "ratio",
		SeedRatio:          1.0,
		MinFreeSpace:       "1GB",
		SearchTimeout:      "15s",
		Theme:              "default",
		ASCII:              false,
		Indexers:           nil,
	}
}
