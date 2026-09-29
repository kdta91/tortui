package indexer

// MeetsMinSeeders reports whether r survives a Query.MinSeeders filter of
// min. Zero or below means no minimum, so everything passes.
//
// A result whose source reported no seeder count at all (marked with
// ExtraKeySeedersUnknown, DEC-138) also passes: its Seeders is a
// placeholder zero, not a measurement, and the swarm may well be healthy.
// A real reported count, zero included, is compared as usual. Every adapter
// that filters locally uses this one function, so the rule cannot drift
// between sources (T-9021).
func MeetsMinSeeders(r Result, min int) bool {
	if min <= 0 {
		return true
	}

	if r.Extra[ExtraKeySeedersUnknown] != "" {
		return true
	}

	return r.Seeders >= min
}
