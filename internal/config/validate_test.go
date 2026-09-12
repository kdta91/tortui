package config

import (
	"strings"
	"testing"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int64
		wantErr bool
	}{
		{name: "plain bytes", in: "1024", want: 1024},
		{name: "kilobytes", in: "2KB", want: 2 << 10},
		{name: "megabytes", in: "1MB", want: 1 << 20},
		{name: "gigabytes", in: "1GB", want: 1 << 30},
		{name: "terabytes", in: "1TB", want: 1 << 40},
		{name: "fractional", in: "1.5GB", want: int64(1.5 * (1 << 30))},
		{name: "lowercase suffix", in: "500mb", want: 500 << 20},
		{name: "whitespace", in: "  1 GB  ", want: 1 << 30},
		{name: "empty", in: "", wantErr: true},
		{name: "garbage", in: "not-a-size", wantErr: true},
		{name: "bad suffix", in: "5XB", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseByteSize(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseByteSize(%q) error = nil, want error", tt.in)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseByteSize(%q) unexpected error: %v", tt.in, err)
			}

			if got != tt.want {
				t.Fatalf("ParseByteSize(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// validConfig returns a Config that Validate accepts unchanged, for tests
// to mutate one field at a time.
func validConfig() Config {
	cfg := Default("/tmp/downloads")
	cfg.Indexers = []Indexer{
		{ID: "example", Name: "Example", Type: "torznab", URL: "https://example.org/api", Enabled: true},
	}

	return cfg
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(c *Config)
		wantEmpty bool
		wantKey   string // substring that must appear in some problem
	}{
		{
			name:      "valid config",
			mutate:    func(c *Config) {},
			wantEmpty: true,
		},
		{
			name:   "partial config still valid (only download_dir customized)",
			mutate: func(c *Config) { c.DownloadDir = "/custom/downloads" },
			// Defaults for everything else keep the config valid.
			wantEmpty: true,
		},
		{
			name:    "empty download_dir",
			mutate:  func(c *Config) { c.DownloadDir = "" },
			wantKey: "download_dir",
		},
		{
			name:    "negative max_download_rate",
			mutate:  func(c *Config) { c.MaxDownloadRate = -1 },
			wantKey: "max_download_rate",
		},
		{
			name:    "negative max_upload_rate",
			mutate:  func(c *Config) { c.MaxUploadRate = -1 },
			wantKey: "max_upload_rate",
		},
		{
			name:    "max_active_downloads too low",
			mutate:  func(c *Config) { c.MaxActiveDownloads = 0 },
			wantKey: "max_active_downloads",
		},
		{
			name:    "max_peers too low",
			mutate:  func(c *Config) { c.MaxPeers = 0 },
			wantKey: "max_peers",
		},
		{
			name:    "listen_port out of range",
			mutate:  func(c *Config) { c.ListenPort = 70000 },
			wantKey: "listen_port",
		},
		{
			name:    "listen_port negative",
			mutate:  func(c *Config) { c.ListenPort = -1 },
			wantKey: "listen_port",
		},
		{
			name:    "invalid seed_policy",
			mutate:  func(c *Config) { c.SeedPolicy = "bogus" },
			wantKey: "seed_policy",
		},
		{
			name:    "negative seed_ratio",
			mutate:  func(c *Config) { c.SeedRatio = -0.5 },
			wantKey: "seed_ratio",
		},
		{
			name:    "unparseable min_free_space",
			mutate:  func(c *Config) { c.MinFreeSpace = "lots" },
			wantKey: "min_free_space",
		},
		{
			name:    "unparseable search_timeout",
			mutate:  func(c *Config) { c.SearchTimeout = "not-a-duration" },
			wantKey: "search_timeout",
		},
		{
			name:    "zero search_timeout",
			mutate:  func(c *Config) { c.SearchTimeout = "0s" },
			wantKey: "search_timeout",
		},
		{
			name:    "empty theme",
			mutate:  func(c *Config) { c.Theme = "" },
			wantKey: "theme",
		},
		{
			name: "indexer missing id",
			mutate: func(c *Config) {
				c.Indexers = append(c.Indexers, Indexer{Name: "No ID", Type: "torznab", URL: "https://example.org"})
			},
			wantKey: "indexer[1].id",
		},
		{
			name: "duplicate indexer id",
			mutate: func(c *Config) {
				c.Indexers = append(c.Indexers, Indexer{
					ID: "example", Name: "Dup", Type: "torznab", URL: "https://example.org",
				})
			},
			wantKey: "indexer[1].id",
		},
		{
			name: "invalid indexer type",
			mutate: func(c *Config) {
				c.Indexers[0].Type = "carrier-pigeon"
			},
			wantKey: "indexer[0].type",
		},
		{
			name: "scraper indexer missing definition",
			mutate: func(c *Config) {
				c.Indexers[0].Type = "scraper"
			},
			wantKey: "indexer[0].definition",
		},
		{
			name: "indexer missing url",
			mutate: func(c *Config) {
				c.Indexers[0].URL = ""
			},
			wantKey: "indexer[0].url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)

			problems := cfg.Validate()

			if tt.wantEmpty {
				if len(problems) != 0 {
					t.Fatalf("Validate() = %v, want no problems", problems)
				}

				return
			}

			found := false
			for _, p := range problems {
				if strings.Contains(p, tt.wantKey) {
					found = true

					break
				}
			}

			if !found {
				t.Fatalf("Validate() = %v, want a problem mentioning %q", problems, tt.wantKey)
			}
		})
	}
}

func TestConfigValidateReturnsAllProblems(t *testing.T) {
	cfg := validConfig()
	cfg.DownloadDir = ""
	cfg.MaxPeers = 0
	cfg.SeedPolicy = "bogus"

	problems := cfg.Validate()
	if len(problems) < 3 {
		t.Fatalf("Validate() = %v, want at least 3 problems (one per invalid field)", problems)
	}
}
