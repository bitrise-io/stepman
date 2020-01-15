package models

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-utils/log"
)

// Semver represents a semantic version
type Semver struct {
	Major, Minor, Patch uint64
}

// String converts a Semver to string
func (v *Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func parseSemver(version string) (Semver, error) {
	versionParts := strings.Split(version, ".")
	if len(versionParts) != 3 {
		return Semver{}, fmt.Errorf("invalid semver version: %s", version)
	}

	major, err := strconv.ParseUint(versionParts[0], 10, 0)
	if err != nil {
		return Semver{}, fmt.Errorf("invalid major version (%s): %s", version, err)
	}
	minor, err := strconv.ParseUint(versionParts[1], 10, 0)
	if err != nil {
		return Semver{}, fmt.Errorf("invalid minor version (%s): %s", version, err)
	}
	patch, err := strconv.ParseUint(versionParts[2], 10, 0)
	if err != nil {
		return Semver{}, fmt.Errorf("invalid patch version (%s): %s", version, err)
	}

	return Semver{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}

// VersionLockType is the type of version lock
type VersionLockType int

const (
	// Fixed is an exact version, e.g. 1.2.5
	Fixed VersionLockType = iota
	// Latest means the latest available version
	Latest
	// MajorLocked means the latest available version with a given major version, e.g. 1.*.*
	MajorLocked
	// MinorLocked means the latest available version with a given major and minor version, e.g. 1.2.*
	MinorLocked
)

// VersionConstraint describes a version and a cosntraint (e.g. use latest major version available)
type VersionConstraint struct {
	VersionLockType VersionLockType
	Version         Semver
}

func parseRequiredVersion(version string) (VersionConstraint, error) {
	parts := strings.Split(version, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return VersionConstraint{}, fmt.Errorf("invalid required version format: %s", version)
	}

	major, err := strconv.ParseUint(parts[0], 10, 0)
	if err != nil {
		return VersionConstraint{}, fmt.Errorf("invalid major version: %s", version)
	}

	if len(parts) == 1 ||
		(len(parts) == 3 &&
			parts[1] == "x" && parts[2] == "x") {
		return VersionConstraint{
			VersionLockType: MajorLocked,
			Version: Semver{
				Major: major,
			},
		}, nil
	}

	minor, err := strconv.ParseUint(parts[1], 10, 0)
	if err != nil {
		return VersionConstraint{}, fmt.Errorf("invalid minor version: %s", version)
	}

	if len(parts) == 2 ||
		(len(parts) == 3 && parts[2] == "x") {
		return VersionConstraint{
			VersionLockType: MinorLocked,
			Version: Semver{
				Major: major,
				Minor: minor,
			},
		}, nil
	}

	patch, err := strconv.ParseUint(parts[2], 10, 0)
	if err != nil {
		return VersionConstraint{}, fmt.Errorf("invalid patch version: %s", version)
	}

	return VersionConstraint{
		VersionLockType: Fixed,
		Version: Semver{
			Major: major,
			Minor: minor,
			Patch: patch,
		},
	}, nil
}

func latestMatchingStepVersion(version VersionConstraint, stepVersions StepGroupModel) (StepVersionModel, bool) {
	switch version.VersionLockType {
	case Fixed:
		{
			version := version.Version.String()
			latestStep, versionFound := stepVersions.Versions[version]

			if !versionFound {
				return StepVersionModel{}, false
			}

			return StepVersionModel{
				Step:                   latestStep,
				Version:                version,
				LatestAvailableVersion: stepVersions.LatestVersionNumber,
			}, true
		}
	case MinorLocked:
		{
			latestVersion := Semver{
				Major: version.Version.Major,
				Minor: version.Version.Minor,
			}
			latestStep := StepModel{}

			for fullVersion, step := range stepVersions.Versions {
				stepVersion, err := parseSemver(fullVersion)
				if err != nil {
					log.Warnf("Invalid step (%s) version: %s", step.Source, fullVersion)
					continue
				}
				if stepVersion.Major != version.Version.Major &&
					stepVersion.Minor != version.Version.Minor {
					continue
				}

				if stepVersion.Patch > latestVersion.Patch {
					latestVersion = stepVersion
					latestStep = step
				}
			}

			return StepVersionModel{
				Step:                   latestStep,
				Version:                latestVersion.String(),
				LatestAvailableVersion: stepVersions.LatestVersionNumber,
			}, true
		}
	case MajorLocked:
		{
			latestStepVersion := Semver{
				Major: version.Version.Major,
			}
			latestStep := StepModel{}

			for fullVersion, step := range stepVersions.Versions {
				stepVersion, err := parseSemver(fullVersion)
				if err != nil {
					log.Warnf("Invalid step (%s) version: %s", step.Source, fullVersion)
					continue
				}
				if stepVersion.Major != version.Version.Major {
					continue
				}

				if stepVersion.Minor > latestStepVersion.Minor ||
					(stepVersion.Minor == latestStepVersion.Minor && stepVersion.Patch > latestStepVersion.Patch) {
					latestStepVersion = stepVersion
					latestStep = step
				}
			}

			return StepVersionModel{
				Step:                   latestStep,
				Version:                latestStepVersion.String(),
				LatestAvailableVersion: stepVersions.LatestVersionNumber,
			}, true
		}
	}

	return StepVersionModel{}, false
}
