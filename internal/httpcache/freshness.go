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
//   - no max-age -> expiresAt == fetchedAt (revalidate before every reuse).
//   - no-cache   -> same as no max-age: revalidate before every reuse.
//   - immutable  -> never revalidate (isFresh short-circuits on it).
//   - no-store   -> not cacheable.
//
// must-revalidate needs no special handling: the cache already revalidates
// every stale entry and surfaces revalidation failures as errors rather than
// serving stale, so the directive's guarantee holds either way.
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
