package stepman

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/bitrise-io/go-utils/cmdex"
	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/go-utils/urlutil"
	"github.com/bitrise-io/go-utils/versions"
	"github.com/bitrise-io/stepman/models"
	"gopkg.in/yaml.v2"
)

// DebugMode ...
var DebugMode bool

// ParseGlobalStepInfoYML ...
func ParseGlobalStepInfoYML(pth string) (models.GlobalStepInfoModel, bool, error) {
	if exist, err := pathutil.IsPathExists(pth); err != nil {
		return models.GlobalStepInfoModel{}, false, err
	} else if !exist {
		return models.GlobalStepInfoModel{}, false, nil
	}

	bytes, err := fileutil.ReadBytesFromFile(pth)
	if err != nil {
		return models.GlobalStepInfoModel{}, true, err
	}

	var globalStepInfo models.GlobalStepInfoModel
	if err := yaml.Unmarshal(bytes, &globalStepInfo); err != nil {
		return models.GlobalStepInfoModel{}, true, err
	}

	return globalStepInfo, true, nil
}

// ParseStepYml ...
func ParseStepYml(pth string, validate bool) (models.StepModel, error) {
	bytes, err := fileutil.ReadBytesFromFile(pth)
	if err != nil {
		return models.StepModel{}, err
	}

	var stepModel models.StepModel
	if err := yaml.Unmarshal(bytes, &stepModel); err != nil {
		return models.StepModel{}, err
	}

	if err := stepModel.Normalize(); err != nil {
		return models.StepModel{}, err
	}

	if validate {
		if err := stepModel.Audit(); err != nil {
			return models.StepModel{}, err
		}
	}

	if err := stepModel.FillMissingDefaults(); err != nil {
		return models.StepModel{}, err
	}

	return stepModel, nil
}

// ParseDeprecationInfo - Parses deprecation-info.yml file and returns the DeprecationInfoModel
func ParseDeprecationInfo(pth string) (models.DeprecationInfoModel, error) {
	bytes, err := fileutil.ReadBytesFromFile(pth)
	if err != nil {
		return models.DeprecationInfoModel{}, err
	}

	var deprecationInfo models.DeprecationInfoModel
	if err := yaml.Unmarshal(bytes, &deprecationInfo); err != nil {
		return models.DeprecationInfoModel{}, err
	}

	return deprecationInfo, nil
}

// ParseAssetsFolder - Creates "asset.ext" - "asset_url" mapping from assets directory content
// assets dir path example:
// * STEPMAN_WORK_DIR/step_collections/COLLECTION_ALIAS/collection/steps/STEP_ID/assets/icon.svg
func ParseAssetsFolder(assetsDirPth, assetsBaseURI, stepID string) (models.AssetURLMap, error) {
	assetsMap := models.AssetURLMap{}

	if !strings.HasSuffix(assetsDirPth, "/") {
		assetsDirPth += "/"
	}

	err := filepath.Walk(assetsDirPth, func(pth string, f os.FileInfo, err error) error {
		// Skip assets dir path (STEPMAN_WORK_DIR/step_collections/COLLECTION_ALIAS/collection/steps/STEP_ID/assets)
		if pth == assetsDirPth {
			return nil
		}

		dir, base := filepath.Split(pth)

		// skip if not a file in assets dir
		if dir != assetsDirPth || base == "" {
			return nil
		}

		assetURI, err := urlutil.Join(assetsBaseURI, stepID, "assets", base)
		if err != nil {
			return err
		}

		assetsMap[base] = assetURI

		return nil
	})

	if err != nil {
		return models.AssetURLMap{}, err
	}

	return assetsMap, nil
}

// ParseStepCollection ...
func ParseStepCollection(pth string) (models.StepCollectionModel, error) {
	bytes, err := fileutil.ReadBytesFromFile(pth)
	if err != nil {
		return models.StepCollectionModel{}, err
	}

	var stepCollection models.StepCollectionModel
	if err := yaml.Unmarshal(bytes, &stepCollection); err != nil {
		return models.StepCollectionModel{}, err
	}
	return stepCollection, nil
}

// DownloadStep ...
func DownloadStep(collectionURI string, collection models.StepCollectionModel, id, version, commithash string) error {
	downloadLocations, err := collection.GetDownloadLocations(id, version)
	if err != nil {
		return err
	}

	route, found := ReadRoute(collectionURI)
	if !found {
		return fmt.Errorf("No routing found for lib: %s", collectionURI)
	}

	stepPth := GetStepCacheDirPath(route, id, version)
	if exist, err := pathutil.IsPathExists(stepPth); err != nil {
		return err
	} else if exist {
		return nil
	}

	success := false
	for _, downloadLocation := range downloadLocations {
		switch downloadLocation.Type {
		case "zip":
			err := retry.Times(2).Wait(3 * time.Second).Try(func(attempt uint) error {
				return cmdex.DownloadAndUnZIP(downloadLocation.Src, stepPth)
			})

			if err != nil {
				log.Warn("Failed to download step.zip: ", err)
			} else {
				success = true
				return nil
			}
		case "git":
			err := retry.Times(2).Wait(3 * time.Second).Try(func(attempt uint) error {
				return cmdex.GitCloneTagOrBranchAndValidateCommitHash(downloadLocation.Src, stepPth, version, commithash)
			})

			if err != nil {
				log.Warnf("Failed to clone step (%s): %v", downloadLocation.Src, err)
			} else {
				success = true
				return nil
			}
		default:
			return fmt.Errorf("Failed to download: Invalid download location (%#v) for step %#v (%#v)", downloadLocation, id, version)
		}
	}

	if !success {
		return errors.New("Failed to download step")
	}
	return nil
}

func addStepVersionToStepGroup(step models.StepModel, version string, stepGroup models.StepGroupModel) (models.StepGroupModel, error) {
	if stepGroup.LatestVersionNumber != "" {
		r, err := versions.CompareVersions(stepGroup.LatestVersionNumber, version)
		if err != nil {
			return models.StepGroupModel{}, err
		}
		if r == 1 {
			stepGroup.LatestVersionNumber = version
		}
	} else {
		stepGroup.LatestVersionNumber = version
	}
	stepGroup.Versions[version] = step
	return stepGroup, nil
}

func generateStepLib(route SteplibRoute, templateCollection models.StepCollectionModel) (models.StepCollectionModel, error) {
	collection := models.StepCollectionModel{
		FormatVersion:         templateCollection.FormatVersion,
		GeneratedAtTimeStamp:  time.Now().Unix(),
		SteplibSource:         templateCollection.SteplibSource,
		DownloadLocations:     templateCollection.DownloadLocations,
		AssetsDownloadBaseURI: templateCollection.AssetsDownloadBaseURI,
	}

	stepHash := models.StepHash{}

	stepsSpecDirPth := GetCollectionBaseDirPath(route)
	err := filepath.Walk(stepsSpecDirPth, func(pth string, f os.FileInfo, err error) error {
		truncatedPath := strings.Replace(pth, stepsSpecDirPth+"/", "", -1)
		match, matchErr := regexp.MatchString("([a-z]+).yml", truncatedPath)
		if matchErr != nil {
			return matchErr
		}

		if match {
			components := strings.Split(truncatedPath, "/")
			if len(components) == 4 {
				stepsDirName := components[0]
				stepID := components[1]
				stepVersion := components[2]

				step, parseErr := ParseStepYml(pth, true)
				if parseErr != nil {
					return parseErr
				}

				// Check for step group deprecation-info.yml - STEP_SPEC_DIR/steps/STEP_ID/deprecation-info.yml
				stepGroupDeprecationInfo := models.DeprecationInfoModel{}
				{
					stepGroupDeprecationInfoPth := filepath.Join(stepsSpecDirPth, stepsDirName, stepID, "deprecation-info.yml")
					if exist, err := pathutil.IsPathExists(stepGroupDeprecationInfoPth); err != nil {
						return err
					} else if exist {
						info, err := ParseDeprecationInfo(stepGroupDeprecationInfoPth)
						if err != nil {
							return err
						}

						stepGroupDeprecationInfo = info
					}
				}

				// Check for step version deprecation-info.yml - STEP_SPEC_DIR/steps/STEP_ID/version/STEP_VERSION/deprecation-info.yml
				{
					stepVersionDeprecationInfo := models.DeprecationInfoModel{}
					stepVersionDeprecationInfoPth := filepath.Join(stepsSpecDirPth, stepsDirName, stepID, "versions", stepVersion, "deprecation-info.yml")
					if exist, err := pathutil.IsPathExists(stepVersionDeprecationInfoPth); err != nil {
						return err
					} else if exist {
						info, err := ParseDeprecationInfo(stepVersionDeprecationInfoPth)
						if err != nil {
							return err
						}

						stepVersionDeprecationInfo = info
						step.Deprecation = stepVersionDeprecationInfo
					}
				}

				// Check for step group assets - STEP_SPEC_DIR/steps/STEP_ID/assets
				stepGroupAssetsMap := models.AssetURLMap{}
				{
					if collection.AssetsDownloadBaseURI != "" {
						assetsDirPth := path.Join(stepsSpecDirPth, stepsDirName, stepID, "assets")
						if exist, err := pathutil.IsPathExists(assetsDirPth); err != nil {
							return err
						} else if exist {
							assetsMap, err := ParseAssetsFolder(assetsDirPth, collection.AssetsDownloadBaseURI, stepID)
							if err != nil {
								return err
							}

							stepGroupAssetsMap = assetsMap
							step.AssetURLs = assetsMap
						}
					}
				}

				// Add infos to step group
				stepGroup, found := stepHash[stepID]
				if !found {
					stepGroup = models.StepGroupModel{
						Versions: map[string]models.StepModel{},
					}
				}

				stepGroup, err = addStepVersionToStepGroup(step, stepVersion, stepGroup)
				if err != nil {
					return err
				}

				stepGroup.AssetURLs = stepGroupAssetsMap
				stepGroup.Deprecation = stepGroupDeprecationInfo

				stepHash[stepID] = stepGroup
			} else {
			}
		}

		return err
	})
	if err != nil {
		log.Error("Failed to walk through path:", err)
		return models.StepCollectionModel{}, err
	}

	collection.Steps = stepHash

	return collection, nil
}

// WriteStepSpecToFile ...
func WriteStepSpecToFile(templateCollection models.StepCollectionModel, route SteplibRoute) error {
	pth := GetStepSpecPath(route)

	if exist, err := pathutil.IsPathExists(pth); err != nil {
		log.Error("Failed to check path:", err)
		return err
	} else if !exist {
		dir, _ := path.Split(pth)
		err := os.MkdirAll(dir, 0777)
		if err != nil {
			return err
		}
	} else {
		err := os.Remove(pth)
		if err != nil {
			return err
		}
	}

	collection, err := generateStepLib(route, templateCollection)
	if err != nil {
		return err
	}

	bytes, err := json.MarshalIndent(collection, "", "\t")
	if err != nil {
		return err
	}
	return fileutil.WriteBytesToFile(pth, bytes)
}

// ReadStepSpec ...
func ReadStepSpec(uri string) (models.StepCollectionModel, error) {

	route, found := ReadRoute(uri)
	if !found {
		return models.StepCollectionModel{}, errors.New("No route found for lib: " + uri)
	}
	pth := GetStepSpecPath(route)
	bytes, err := fileutil.ReadBytesFromFile(pth)
	if err != nil {
		return models.StepCollectionModel{}, err
	}
	var stepLib models.StepCollectionModel
	if err := json.Unmarshal(bytes, &stepLib); err != nil {
		return models.StepCollectionModel{}, err
	}
	return stepLib, nil
}

// ReGenerateStepSpec ...
func ReGenerateStepSpec(route SteplibRoute) error {
	pth := GetCollectionBaseDirPath(route)
	if exists, err := pathutil.IsPathExists(pth); err != nil {
		return err
	} else if !exists {
		return errors.New("Not initialized")
	}

	specPth := pth + "/steplib.yml"
	collection, err := ParseStepCollection(specPth)
	if err != nil {
		return err
	}

	if err := WriteStepSpecToFile(collection, route); err != nil {
		return err
	}
	return nil
}
