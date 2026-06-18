package steplibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/pointers"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/steplibrary/steplibindex"
)

// fakeAPI is an in-memory API implementation driven entirely by its map
// fixtures and injectable errors. Construct the standard "script" fixtures with
// newFakeAPI; override individual fields for table-driven and error cases.
type fakeAPI struct {
	ids               []string
	listErr           error
	latestVersions    map[string]steplibindex.LatestPointer
	latestVersionsErr error
	allVersions       map[string][]string
	allVersionsErr    error
	groupInfo         map[string]steplibindex.StepInfo
	groupInfoErr      error
	stepModel         map[string]models.StepModel
}

// newFakeAPI returns a fakeAPI pre-populated with the standard "script" step
// fixtures (versions 1.0.0–3.0.0, latest 3.0.0, bitrise maintainer, a minimal
// step model).
func newFakeAPI() fakeAPI {
	return fakeAPI{
		ids: []string{"xcode-test", "script"},
		latestVersions: map[string]steplibindex.LatestPointer{
			"script": {
				StepID:        "script",
				Latest:        "3.0.0",
				LatestByMajor: map[string]string{"1": "1.2.0", "2": "2.4.1", "3": "3.0.0"},
			},
		},
		allVersions: map[string][]string{
			"script": {"1.0.0", "1.1.5", "1.2.0", "2.0.0", "2.4.0", "2.4.1", "3.0.0"},
		},
		groupInfo: map[string]steplibindex.StepInfo{
			"script": {Maintainer: "bitrise", Deprecation: nil, AssetURLs: []string{"assets/icon.svg"}},
		},
		stepModel: map[string]models.StepModel{
			"script": {Title: pointers.NewStringPtr("Script"), Summary: pointers.NewStringPtr("Runs a shell script.")},
		},
	}
}

func (f fakeAPI) GetAllStepIDs(_ context.Context) ([]string, error) {
	return f.ids, f.listErr
}

func (f fakeAPI) GetLatestStepVersions(_ context.Context, id string) (steplibindex.LatestPointer, error) {
	if f.latestVersionsErr != nil {
		return steplibindex.LatestPointer{}, f.latestVersionsErr
	}
	v, ok := f.latestVersions[id]
	if !ok {
		return steplibindex.LatestPointer{}, errors.New("not found")
	}
	return v, nil
}

func (f fakeAPI) GetAllStepVersions(_ context.Context, id string) ([]string, error) {
	if f.allVersionsErr != nil {
		return nil, f.allVersionsErr
	}
	v, ok := f.allVersions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (f fakeAPI) GetStepGroupInfo(_ context.Context, id string) (steplibindex.StepInfo, error) {
	if f.groupInfoErr != nil {
		return steplibindex.StepInfo{}, f.groupInfoErr
	}
	v, ok := f.groupInfo[id]
	if !ok {
		return steplibindex.StepInfo{}, errors.New("not found")
	}
	return v, nil
}

func (f fakeAPI) GetStepModel(_ context.Context, step ResolvedStepVersion) (models.StepModel, error) {
	v, ok := f.stepModel[step.ID]
	if !ok {
		return models.StepModel{}, errors.New("not found")
	}
	return v, nil
}

// fakeFetcher is an httpfetch.Client that writes a fixed payload.
type fakeFetcher struct {
	payload []byte
	gotURL  string
	err     error
}

func (f *fakeFetcher) Get(_ context.Context, source string) (io.ReadCloser, error) {
	return nil, errors.New("Get not used by Steplib precompiled flow")
}

func (f *fakeFetcher) Download(_ context.Context, _, _ string) error {
	return errors.New("Download not used by Steplib precompiled flow")
}

func (f *fakeFetcher) DownloadWithHash(_ context.Context, destPath, url, expectedHash string) error {
	f.gotURL = url
	if f.err != nil {
		return f.err
	}
	actual := sha256OfBytes(f.payload)
	if actual != expectedHash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actual)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destPath, f.payload, 0o644)
}

// fakeGetFetcher implements httpfetch.Client.Get, returning a fixed body whose
// Close returns closeErr. Used to exercise fetchJSON's close-error path.
type fakeGetFetcher struct {
	body     string
	closeErr error
}

func (f fakeGetFetcher) Get(_ context.Context, _ string) (io.ReadCloser, error) {
	return errReadCloser{Reader: strings.NewReader(f.body), closeErr: f.closeErr}, nil
}

func (f fakeGetFetcher) Download(_ context.Context, _, _ string) error {
	return errors.New("Download not used")
}

func (f fakeGetFetcher) DownloadWithHash(_ context.Context, _, _, _ string) error {
	return errors.New("DownloadWithHash not used")
}

// errReadCloser wraps a reader and returns closeErr from Close.
type errReadCloser struct {
	io.Reader
	closeErr error
}

func (e errReadCloser) Close() error { return e.closeErr }

func sha256OfBytes(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256-" + hex.EncodeToString(h[:])
}

// testLogger routes stepman log output to t.Log.
type testLogger struct{ t *testing.T }

func (l testLogger) Debugf(f string, a ...any) { l.t.Logf("DEBUG "+f, a...) }
func (l testLogger) Errorf(f string, a ...any) { l.t.Logf("ERROR "+f, a...) }
func (l testLogger) Warnf(f string, a ...any)  { l.t.Logf("WARN "+f, a...) }
func (l testLogger) Infof(f string, a ...any)  { l.t.Logf("INFO "+f, a...) }
