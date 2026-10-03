package engine

import (
	"net/url"
	"strings"
)

// WithoutMetainfoSources drops a magnet's xs= and as= parameters, which make
// a torrent library fetch the .torrent over HTTP from whatever host they
// name, with its own client: outside tortui's HTTP client, and outside an
// offline engine. tortui connects only to its sources and to BitTorrent peers
// (AGENT.md §2), so no magnet keeps them, whoever wrote it (DEC-151,
// DEC-153); trackers, web seeds and peers are BitTorrent and stay. Every
// other parameter is kept byte for byte. A key is compared decoded, as the
// library reads it.
//
// The engine runs every magnet it accepts through this; the add flow runs
// the magnet it records through it too, so the first record write never
// holds them either (T-9097, DEC-161).
func WithoutMetainfoSources(magnet string) string {
	head, query, found := strings.Cut(magnet, "?")
	if !found {
		return magnet
	}

	params := strings.Split(query, "&")
	kept := params[:0]

	for _, param := range params {
		raw, _, _ := strings.Cut(param, "=")

		key, err := url.QueryUnescape(raw)
		if err == nil && (key == "xs" || key == "as") {
			continue
		}

		kept = append(kept, param)
	}

	return head + "?" + strings.Join(kept, "&")
}
