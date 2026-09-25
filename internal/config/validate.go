package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var byteSizePattern = regexp.MustCompile(`(?i)^\s*([0-9]*\.?[0-9]+)\s*(B|KB|MB|GB|TB)?\s*$`)

// ParseByteSize parses a human-readable byte size such as "1GB", "500MB",
// "1.5GB", or a plain byte count such as "1048576", returning the value in
// bytes. Suffixes are case-insensitive and use binary (1024-based) units.
func ParseByteSize(s string) (int64, error) {
	m := byteSizePattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid size %q (want e.g. 500MB, 1GB, or a plain byte count)", s)
	}

	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}

	var mult float64
	switch strings.ToUpper(m[2]) {
	case "", "B":
		mult = 1
	case "KB":
		mult = 1 << 10
	case "MB":
		mult = 1 << 20
	case "GB":
		mult = 1 << 30
	case "TB":
		mult = 1 << 40
	}

	return int64(val * mult), nil
}

// Validate checks c for problems and returns every one it finds, each
// naming the offending key, rather than stopping at the first. A nil or
// empty return means the configuration is usable as-is.
func (c Config) Validate() []string {
	var problems []string

	if strings.TrimSpace(c.DownloadDir) == "" {
		problems = append(problems, "download_dir: must not be empty")
	}

	if c.MaxActiveDownloads < 1 {
		problems = append(problems, "max_active_downloads: must be at least 1")
	}

	if c.MaxDownloadRate < 0 {
		problems = append(problems, "max_download_rate: must not be negative")
	}

	if c.MaxUploadRate < 0 {
		problems = append(problems, "max_upload_rate: must not be negative")
	}

	if c.MaxPeers < 1 {
		problems = append(problems, "max_peers: must be at least 1")
	}

	if c.ListenPort < 0 || c.ListenPort > 65535 {
		problems = append(problems, "listen_port: must be between 0 and 65535")
	}

	switch c.SeedPolicy {
	case "ratio", "duration", "off":
	default:
		problems = append(problems, fmt.Sprintf("seed_policy: invalid value %q (want ratio, duration, or off)", c.SeedPolicy))
	}

	if c.SeedRatio < 0 {
		problems = append(problems, "seed_ratio: must not be negative")
	}

	if d, err := time.ParseDuration(c.SeedDuration); err != nil {
		problems = append(problems, fmt.Sprintf("seed_duration: %v", err))
	} else if d <= 0 {
		problems = append(problems, "seed_duration: must be positive")
	}

	if _, err := ParseByteSize(c.MinFreeSpace); err != nil {
		problems = append(problems, fmt.Sprintf("min_free_space: %v", err))
	}

	if d, err := time.ParseDuration(c.SearchTimeout); err != nil {
		problems = append(problems, fmt.Sprintf("search_timeout: %v", err))
	} else if d <= 0 {
		problems = append(problems, "search_timeout: must be positive")
	}

	if strings.TrimSpace(c.Theme) == "" {
		problems = append(problems, "theme: must not be empty")
	}

	seenIDs := make(map[string]bool, len(c.Indexers))
	for i, idx := range c.Indexers {
		prefix := fmt.Sprintf("indexer[%d]", i)

		switch {
		case idx.ID == "":
			problems = append(problems, prefix+".id: must not be empty")
		case seenIDs[idx.ID]:
			problems = append(problems, fmt.Sprintf("%s.id: duplicate id %q", prefix, idx.ID))
		default:
			seenIDs[idx.ID] = true
		}

		if idx.Name == "" {
			problems = append(problems, prefix+".name: must not be empty")
		}

		switch idx.Type {
		case "torznab", "scraper":
		default:
			problems = append(problems, fmt.Sprintf("%s.type: invalid value %q (want torznab or scraper)", prefix, idx.Type))
		}

		if idx.URL == "" {
			problems = append(problems, prefix+".url: must not be empty")
		}

		if idx.Type == "scraper" && idx.Definition == "" {
			problems = append(problems, prefix+".definition: required for scraper indexers")
		}
	}

	return problems
}
