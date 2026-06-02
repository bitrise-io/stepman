package sri

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// knownHash is the SHA-256 of "hello world", in this package's "sha256-<hex>"
// format.
const knownHash = "sha256-b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

func TestSHA256(t *testing.T) {
	assert.Equal(t, knownHash, SHA256([]byte("hello world")))
}

func TestSHA256Reader(t *testing.T) {
	got, err := SHA256Reader(strings.NewReader("hello world"))
	require.NoError(t, err)
	assert.Equal(t, knownHash, got)
}

func TestSHA256AndReaderAgree(t *testing.T) {
	data := []byte("the quick brown fox")
	fromReader, err := SHA256Reader(strings.NewReader(string(data)))
	require.NoError(t, err)
	assert.Equal(t, SHA256(data), fromReader)
	assert.True(t, strings.HasPrefix(SHA256(data), Prefix))
}
