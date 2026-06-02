package httpcache

import (
	"time"

	"github.com/pquerna/cachecontrol/cacheobject"
)

// parseCacheControl interprets a response Cache-Control header into the fields
// the cache needs: when the entry stops being fresh, whether it is immutable,
// and whether it must not be stored at all.
//
//   - max-age=N  -> fresh for N seconds from fetchedAt.
//   - no max-age -> expiresAt == fetchedAt (always revalidated; still storable
//     so it can act as a stale fallback).
//   - no-cache   -> same as no max-age: revalidate before every reuse.
//   - immutable  -> never revalidate (isFresh short-circuits on it).
//   - no-store   -> not cacheable.
//
// must-revalidate is intentionally NOT treated as "immediately stale": it only
// governs the freshness window's edge, and stepman deliberately keeps stale
// entries as a last-resort fallback even when the server asks not to (a CDN /
// network outage must not break resolution).
func parseCacheControl(header string, fetchedAt time.Time) (expiresAt time.Time, immutable, noStore bool) {
	if header == "" {
		return fetchedAt, false, false
	}

	directives, err := cacheobject.ParseResponseCacheControl(header)
	if err != nil {
		// Unparseable directive: keep it storable but always revalidate.
		return fetchedAt, false, false
	}
	if directives.NoStore {
		return fetchedAt, false, true
	}

	expiresAt = fetchedAt
	if directives.MaxAge >= 0 {
		expiresAt = fetchedAt.Add(time.Duration(directives.MaxAge) * time.Second)
	}
	if directives.NoCachePresent {
		expiresAt = fetchedAt
	}
	return expiresAt, directives.Immutable, false
}
