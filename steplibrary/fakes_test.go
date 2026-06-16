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

// FakeAPI is an in-memory API implementation used as a base for test fakes
// that embed it and selectively override methods.
type FakeAPI struct{}

func (m FakeAPI) GetAllStepIDs(_ context.Context) ([]string, error) {
	return []string{"xcode-test", "script"}, nil
}

func (m FakeAPI) GetLatestStepVersions(_ context.Context, id string) (steplibindex.LatestPointer, error) {
	versions := map[string]steplibindex.LatestPointer{
		"script": {
			StepID: "script",
			Latest: "3.0.0",
			LatestByMajor: map[string]string{
				"1": "1.2.0",
				"2": "2.4.1",
				"3": "3.0.0",
			},
		},
	}

	v, ok := versions[id]
	if !ok {
		return steplibindex.LatestPointer{}, errors.New("not found")
	}
	return v, nil
}

func (m FakeAPI) GetAllStepVersions(_ context.Context, id string) ([]string, error) {
	versions := map[string][]string{
		"script": {"1.0.0", "1.1.5", "1.2.0", "2.0.0", "2.4.0", "2.4.1", "3.0.0"},
	}
	v, ok := versions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (m FakeAPI) GetStepGroupInfo(_ context.Context, id string) (steplibindex.StepInfo, error) {
	infos := map[string]steplibindex.StepInfo{
		"script": {
			Maintainer:  "bitrise",
			Deprecation: nil,
			AssetURLs:   []string{"assets/icon.svg"},
		},
	}
	v, ok := infos[id]
	if !ok {
		return steplibindex.StepInfo{}, errors.New("not found")
	}
	return v, nil
}

func (m FakeAPI) GetStepModel(_ context.Context, step ResolvedStepVersion) (models.StepModel, error) {
	if step.ID != "script" {
		return models.StepModel{}, errors.New("not found")
	}
	//nolint:exhaustruct // mock returns a minimal StepModel; downstream consumers don't need the full shape here
	return models.StepModel{
		Title:   pointers.NewStringPtr("Script"),
		Summary: pointers.NewStringPtr("Runs a shell script."),
	}, nil
}

// fakeAPI embeds FakeAPI and overrides methods with table-driven fixtures and
// injectable errors for the Activate/resolve tests.
type fakeAPI struct {
	FakeAPI
	ids               []string
	listErr           error
	latestVersions    map[string]steplibindex.LatestPointer
	latestVersionsErr error
	allVersions       map[string][]string
	allVersionsErr    error
	groupInfoErr      error
	stepModel         map[string]models.StepModel
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

func (f fakeAPI) GetStepGroupInfo(ctx context.Context, id string) (steplibindex.StepInfo, error) {
	if f.groupInfoErr != nil {
		return steplibindex.StepInfo{}, f.groupInfoErr
	}
	return f.FakeAPI.GetStepGroupInfo(ctx, id)
}

func (f fakeAPI) GetStepModel(ctx context.Context, step ResolvedStepVersion) (models.StepModel, error) {
	if f.stepModel != nil {
		v, ok := f.stepModel[step.ID]
		if !ok {
			return models.StepModel{}, errors.New("not found")
		}
		return v, nil
	}
	return f.FakeAPI.GetStepModel(ctx, step)
}

// fakeFetcher implements httpfetch.Client by writing a fixed byte payload on
// DownloadWithHash. Get and Download are not used by the precompiled flow.
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

// stubSource is a sourceProvider that returns a fixed dir/err, standing in for
// the V1-cache-backed v1Source in activation tests.
type stubSource struct {
	dir string
	err error
}

func (s stubSource) stepSourceDir(context.Context, ResolvedStepVersion) (string, error) {
	return s.dir, s.err
}

// testLog routes stepman log output to t.Log, so activation stays quiet on
// success but surfaces (e.g. precompiled-fallback warnings) in failure output.
type testLog struct{ t *testing.T }

func (l testLog) Debugf(f string, a ...any) { l.t.Logf("DEBUG "+f, a...) }
func (l testLog) Errorf(f string, a ...any) { l.t.Logf("ERROR "+f, a...) }
func (l testLog) Warnf(f string, a ...any)  { l.t.Logf("WARN "+f, a...) }
func (l testLog) Infof(f string, a ...any)  { l.t.Logf("INFO "+f, a...) }

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

// writeSeedDir creates a directory containing a single step source file, used
// as a stand-in for the V1 cache dir that getStepSourceDir returns.
func writeSeedDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create seed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "step.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
}
