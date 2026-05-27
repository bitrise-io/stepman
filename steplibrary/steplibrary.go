package steplibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/v2/fileutil"
	v2log "github.com/bitrise-io/go-utils/v2/log"
	"github.com/bitrise-io/stepman/activator/result"
	"github.com/bitrise-io/stepman/internal/httpfetch"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
	"gopkg.in/yaml.v2"
)

type Steplib struct {
	log           stepman.Logger
	steplibURI    string
	isOfflineMode bool
	api           API
	fileManager   fileutil.FileManager
	fetcher       httpfetch.Client
}

type ActivateOutputPaths struct {
	YMLPath, CodePath string
}

func New(log stepman.Logger, steplibURI string, isOfflineMode bool, fileManager fileutil.FileManager) *Steplib {
	api := NewHTTPAPI(steplibURI, v2CacheDir(steplibURI), nil)
	return &Steplib{
		log:           log,
		steplibURI:    steplibURI,
		isOfflineMode: isOfflineMode,
		api:           api,
		fileManager:   fileManager,
		// Used for downloading precompiled step binaries from arbitrary URLs
		// (storage_uri is rooted at a different host than the inventory).
		fetcher: httpfetch.NewClient(nil, v2log.NewLogger(v2log.WithOutput(io.Discard))),
	}
}

// v2CacheDir returns a stable on-disk cache directory for a given steplib URL.
// Keyed by a sha256 prefix so different URLs don't collide and the directory
// name is filesystem-safe.
func v2CacheDir(steplibURI string) string {
	sum := sha256.Sum256([]byte(steplibURI))
	return filepath.Join(stepman.GetStepmanDirPath(), "v2-cache", hex.EncodeToString(sum[:8]))
}

func (s *Steplib) Activate(ctx context.Context, stepID, version string, outputPaths ActivateOutputPaths) (result.ActivatedStep, error) {
	stepInfo, resolved, err := s.getStepVersionInfo(ctx, stepID, version)

	var stepModel models.StepModel
	var execPath string
	if err == nil {
		stepModel, err = s.api.GetStepModel(ctx, resolved)
	}

	// Prefer the precompiled binary for the current platform when the step
	// publishes one; transparently fall back to source on any failure so an
	// individual broken executable can't block activation.
	if err == nil {
		if executable, ok := resolveExecutable(stepModel); ok {
			path, perr := s.downloadPrecompiled(ctx, stepID, executable, outputPaths.CodePath)
			if perr == nil {
				execPath = path
			} else {
				s.log.Warnf("Failed to download precompiled binary for %s, falling back to source: %s", currentPlatform(), perr)
			}
		}
	}

	if err == nil && execPath == "" {
		var stepSourceZIPPath string
		stepSourceZIPPath, err = s.api.GetStepSourceZIPPath(ctx, resolved)
		if err == nil {
			if uerr := command.UnZIP(stepSourceZIPPath, outputPaths.CodePath); uerr != nil {
				err = fmt.Errorf("unzip step source %s: %w", stepSourceZIPPath, uerr)
			}
		}
	}
	var stepYML []byte
	if err == nil {
		stepYML, err = yaml.Marshal(stepModel)
		if err != nil {
			err = fmt.Errorf("marshal step model to YAML: %w", err)
		}
	}
	if err == nil {
		err = s.fileManager.WriteBytes(outputPaths.YMLPath, stepYML)
	}
	if err != nil {
		return result.ActivatedStep{}, err
	}

	activationType := result.ActivationTypeSteplibSource
	if execPath != "" {
		activationType = result.ActivationTypeSteplibExecutable
	}
	return result.ActivatedStep{
		StepInfo:         stepInfo,
		StepYMLPath:      outputPaths.YMLPath,
		ExecutablePath:   execPath,
		ActivationType:   activationType,
		DidStepLibUpdate: false, // deprecated
	}, nil
}

func (s *Steplib) getStepVersionInfo(ctx context.Context, stepID, version string) (models.StepInfoModel, ResolvedStepVersion, error) {
	var err error
	if stepID == "" {
		err = errors.New("missing required input: step id")
	}

	var allSteps []string
	if err == nil {
		allSteps, err = s.api.GetAllStepIDs(ctx)
		if err != nil {
			err = fmt.Errorf("fetching avaialble step IDs: %w", err)
		}
	}
	if err == nil && !slices.Contains(allSteps, stepID) {
		err = fmt.Errorf("%s steplib does not contain %s step", s.steplibURI, stepID)
	}

	var versionConstraint models.VersionConstraint
	if err == nil {
		versionConstraint, err = models.ParseRequiredVersion(version)
		if err != nil {
			err = fmt.Errorf("invalid step version constraint: %w", err)
		}
	}
	if err == nil && versionConstraint.VersionLockType == models.InvalidVersionConstraint {
		err = fmt.Errorf("invalid step version constraint: %s", version)
	}

	var latestVersions StepVersionsLatest
	if err == nil {
		latestVersions, err = s.api.GetLatestStepVersions(ctx, stepID)
		if err != nil {
			err = fmt.Errorf("fetching latest versions of `%s`: %w", stepID, err)
		}
	}

	var groupInfo StepGroupInfo
	if err == nil {
		groupInfo, err = s.api.GetStepGroupInfo(ctx, stepID)
		if err != nil {
			err = fmt.Errorf("fetching group info of `%s`: %w", stepID, err)
		}
	}

	var resolvedVersion string
	if err == nil {
		switch versionConstraint.VersionLockType {
		case models.Latest:
			resolvedVersion = latestVersions.Latest
		case models.Fixed:
			resolvedVersion = versionConstraint.Version.String()
			// ToDo: check version exists, otherwise error:
			// "%s steplib does not contain %s step %s version"
		case models.MajorLocked:
			majorKey := strconv.FormatUint(versionConstraint.Version.Major, 10)
			v, ok := latestVersions.LatestByMajor[majorKey]
			if !ok {
				err = fmt.Errorf("%s steplib does not contain %s step with major version %s", s.steplibURI, stepID, majorKey)
			} else {
				resolvedVersion = v
			}
		case models.MinorLocked:
			var allVersions []string
			allVersions, err = s.api.GetAllStepVersions(ctx, stepID)
			if err != nil {
				err = fmt.Errorf("fetching all versions of `%s`: %w", stepID, err)
			}
			if err == nil {
				resolvedVersion, err = resolveMinorLocked(allVersions, versionConstraint.Version)
				if err != nil {
					err = fmt.Errorf("%s steplib: %w", s.steplibURI, err)
				}
			}
		default:
			err = fmt.Errorf("unknown version constraint: %s", version)
		}
	}

	if err != nil {
		return models.StepInfoModel{}, ResolvedStepVersion{}, err
	}
	//nolint:exhaustruct // Step and DefinitionPth aren't surfaced by the v2 API yet
	return models.StepInfoModel{
		Library:         s.steplibURI,
		ID:              stepID,
		Version:         resolvedVersion,
		OriginalVersion: version,
		LatestVersion:   latestVersions.Latest,
		GroupInfo:       toStepGroupInfoModel(groupInfo),
	}, ResolvedStepVersion{ID: stepID, Version: resolvedVersion}, nil
}

// toStepGroupInfoModel flattens v2's nested `deprecation` object into v1's
// `RemovalDate` + `DeprecateNotes` fields so the rest of the codebase keeps
// reading the same model shape.
func toStepGroupInfoModel(info StepGroupInfo) models.StepGroupInfoModel {
	out := models.StepGroupInfoModel{
		Maintainer:     info.Maintainer,
		AssetURLs:      info.AssetURLs,
		RemovalDate:    "",
		DeprecateNotes: "",
	}
	if info.Deprecation != nil {
		out.RemovalDate = info.Deprecation.RemovalDate
		out.DeprecateNotes = info.Deprecation.Notes
	}
	return out
}

// resolveMinorLocked picks the highest patch within `versions` matching the
// constraint's Major+Minor. Unparseable entries are skipped.
func resolveMinorLocked(versions []string, constraint models.Semver) (string, error) {
	var best models.Semver
	found := false
	for _, raw := range versions {
		sv, err := models.ParseSemver(raw)
		if err != nil {
			continue
		}
		if sv.Major != constraint.Major || sv.Minor != constraint.Minor {
			continue
		}
		if !found || sv.Patch > best.Patch {
			best = sv
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("no version matches %d.%d.x", constraint.Major, constraint.Minor)
	}
	return best.String(), nil
}
