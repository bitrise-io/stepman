package steplibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/bitrise-io/go-utils/v2/fileutil"
	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/stepman"
)

type Steplib struct {
	log              stepman.Logger
	steplibURI       string
	isOfflineMode    bool
	api              API
	fileManager      fileutil.FileManager
	fetcher          httpfetch.Client
	fetchSourceDirFn func(ctx context.Context, step ResolvedStepVersion) (string, error)
}

type ActivateOutputPaths struct {
	YMLPath, CodePath string
}

func New(log stepman.Logger, steplibURI string, isOfflineMode bool, fileManager fileutil.FileManager) *Steplib {
	api := NewHTTPAPI(steplibURI, v2CacheDir(steplibURI), nil, log)
	s := &Steplib{
		log:              log,
		steplibURI:       steplibURI,
		isOfflineMode:    isOfflineMode,
		api:              api,
		fileManager:      fileManager,
		fetcher:          httpfetch.NewClient(nil, log),
		fetchSourceDirFn: nil,
	}
	s.fetchSourceDirFn = s.getStepSourceDir
	return s
}

// v2CacheDir returns a stable on-disk cache directory for a given steplib URL.
// Keyed by a sha256 prefix so different URLs don't collide and the directory
// name is filesystem-safe.
func v2CacheDir(steplibURI string) string {
	sum := sha256.Sum256([]byte(steplibURI))
	return filepath.Join(stepman.GetStepmanDirPath(), "v2-cache", hex.EncodeToString(sum[:8]))
}
