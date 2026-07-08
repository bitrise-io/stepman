// Package sri produces and formats Subresource-Integrity-style content
// digests of the form "sha256-<hex>", the single hash format stepman uses for
// precompiled binaries, V2 step manifests, and cached HTTP bodies.
package sri

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// Prefix is the algorithm label on every digest this package produces.
const Prefix = "sha256-"

// Format renders a raw digest as "sha256-<hex>".
func Format(sum []byte) string {
	return Prefix + hex.EncodeToString(sum)
}

// SHA256 returns the "sha256-<hex>" digest of data.
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return Format(sum[:])
}

// SHA256Reader returns the "sha256-<hex>" digest of everything read from r.
func SHA256Reader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return Format(h.Sum(nil)), nil
}
