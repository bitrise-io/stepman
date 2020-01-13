package models

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-utils/log"
)

type semver struct {
	major, minor, patch uint64
}

func (v *semver) toString() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func parseSemver(version string) (semver, error) {
	versionParts := strings.Split(version, ".")
	if len(versionParts) != 3 {
		return semver{}, fmt.Errorf("Invalid semver version: %s", version)
	}

	major, err := strconv.ParseUint(versionParts[0], 10, 0)
	if err != nil {
		return semver{}, fmt.Errorf("Invalid major version (%s), error: %s", version, err)
	}
	minor, err := strconv.ParseUint(versionParts[1], 10, 0)
	if err != nil {
		return semver{}, fmt.Errorf("Invalid minor version (%s), error: %s", version, err)
	}
	patch, err := strconv.ParseUint(versionParts[2], 10, 0)
	if err != nil {
		return semver{}, fmt.Errorf("Invalid patch version (%s), error: %s", version, err)
	}

	return semver{
		major: major,
		minor: minor,
		patch: patch,
	}, nil
}

type versionLockType int

const (
	fixed versionLockType = iota
	majorLocked
	minorLocked
)

type versionConstraint struct {
	versionLockType versionLockType
	version         semver
}

func parseRequiredVersion(version string) (versionConstraint, error) {
	requiredVersionParts := strings.Split(version, ".")
	if len(requiredVersionParts) == 0 || len(requiredVersionParts) > 3 {
		return versionConstraint{}, fmt.Errorf("Invalid required version format: %s", version)
	}

	requiredMajor, err := strconv.ParseUint(requiredVersionParts[0], 10, 0)
	if err != nil {
		return versionConstraint{}, fmt.Errorf("Invalid major version: %s", version)
	}

	if len(requiredVersionParts) == 1 ||
		(len(requiredVersionParts) == 3 &&
			requiredVersionParts[1] == "x" && requiredVersionParts[2] == "x") {
		return versionConstraint{
			versionLockType: majorLocked,
			version: semver{
				major: requiredMajor,
			},
		}, nil
	}

	requiredMinor, err := strconv.ParseUint(requiredVersionParts[1], 10, 0)
	if err != nil {
		return versionConstraint{}, fmt.Errorf("Invalid minor version: %s", version)
	}

	if len(requiredVersionParts) == 2 ||
		(len(requiredVersionParts) == 3 && requiredVersionParts[2] == "x") {
		return versionConstraint{
			versionLockType: minorLocked,
			version: semver{
				major: requiredMajor,
				minor: requiredMinor,
			},
		}, nil
	}

	requiredPatch, err := strconv.ParseUint(requiredVersionParts[2], 10, 0)
	if err != nil {
		return versionConstraint{}, fmt.Errorf("Invalid patch version: %s", version)
	}

	return versionConstraint{
		versionLockType: fixed,
		version: semver{
			major: requiredMajor,
			minor: requiredMinor,
			patch: requiredPatch,
		},
	}, nil
}

func latestMatchingRequiredVersion(version versionConstraint, stepVersions StepGroupModel) (StepVersionModel, bool) {
	switch version.versionLockType {
	case fixed:
		{
			version := version.version.toString()
			bestMatchingStep, versionFound := stepVersions.Versions[version]

			if !versionFound {
				return StepVersionModel{}, false
			}

			return StepVersionModel{
				Step:                   bestMatchingStep,
				Version:                version,
				LatestAvailableVersion: stepVersions.LatestVersionNumber,
			}, true
		}
	case minorLocked:
		{
			bestMatchingVersion := semver{
				major: version.version.major,
				minor: version.version.minor,
			}
			bestMatchingStep := StepModel{}

			for fullVersion, step := range stepVersions.Versions {
				stepVersion, err := parseSemver(fullVersion)
				if err != nil {
					log.Warnf("Invalid step (%s) version: %s", step.Source, fullVersion)
					continue
				}
				if stepVersion.major != version.version.major &&
					stepVersion.minor != version.version.minor {
					continue
				}

				if stepVersion.patch > bestMatchingVersion.patch {
					bestMatchingVersion = stepVersion
					bestMatchingStep = step
				}
			}

			return StepVersionModel{
				Step:                   bestMatchingStep,
				Version:                bestMatchingVersion.toString(),
				LatestAvailableVersion: stepVersions.LatestVersionNumber,
			}, true
		}
	case majorLocked:
		{
			bestMatchingVersion := semver{
				major: version.version.major,
			}
			bestMatchingStep := StepModel{}

			for fullVersion, step := range stepVersions.Versions {
				stepVersion, err := parseSemver(fullVersion)
				if err != nil {
					log.Warnf("Invalid step (%s) version: %s", step.Source, fullVersion)
					continue
				}
				if stepVersion.major != version.version.major {
					continue
				}

				if stepVersion.minor > bestMatchingVersion.minor ||
					(stepVersion.minor == bestMatchingVersion.minor && stepVersion.patch > bestMatchingVersion.patch) {
					bestMatchingVersion = stepVersion
					bestMatchingStep = step
				}
			}

			return StepVersionModel{
				Step:                   bestMatchingStep,
				Version:                bestMatchingVersion.toString(),
				LatestAvailableVersion: stepVersions.LatestVersionNumber,
			}, true
		}
	}

	return StepVersionModel{}, false
}
