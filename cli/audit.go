package cli

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	logrus "github.com/Sirupsen/logrus"
	"github.com/bitrise-io/go-utils/colorstring"
	"github.com/bitrise-io/go-utils/command/git"
	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
	"github.com/bitrise-io/stepman/validate"
	"github.com/urfave/cli"
	yaml "gopkg.in/yaml.v2"
)

var auditStepCommand = cli.Command{
	Name:  "audit-step",
	Usage: "Validates a Step and Step Share Params (id, version, uri).",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "uri",
			Usage: "The Step repository's git URI.",
		},
		cli.StringFlag{
			Name:  "id",
			Usage: "The Step ID.",
		},
		cli.StringFlag{
			Name:  "version",
			Usage: "The Step version.",
		},
		cli.StringFlag{
			Name:  "dir",
			Usage: "The Step's directory",
		},
	},
	Action: func(c *cli.Context) error {
		if err := auditStep(c); err != nil {
			logrus.Fatal(err)
		}
		return nil
	},
}

func auditStep(c *cli.Context) error {
	uri := c.String("uri")
	id := c.String("id")
	version := c.String("version")
	dir := c.String("dir")

	fmt.Println()
	log.Infof("Audting Step:")
	log.Printf("- Step repository uri: %s", uri)
	log.Printf("- Step id: %s", id)
	log.Printf("- Step version: %s", version)
	log.Printf("- Step dir: %s", dir)
	fmt.Println()

	if uri == "" {
		return errors.New("step uri not specified")
	}
	if id == "" {
		return errors.New("step id not specified")
	}
	if version == "" {
		return errors.New("step version not specified")
	}
	if dir == "" {
		return errors.New("step dir not specified")
	}

	if err := validate.StepParams(uri, id, version); err != nil {
		return err
	}

	if err := validate.StepRepo(dir); err != nil {
		return err
	}

	definitionPth := path.Join(dir, "step.yml")
	bytes, err := fileutil.ReadBytesFromFile(definitionPth)
	if err != nil {
		return fmt.Errorf("Failed to read Step from file, err: %s", err)
	}

	var step models.StepModel
	if err := yaml.Unmarshal(bytes, &step); err != nil {
		return fmt.Errorf("Failed to unmarchal Step, err: %s", err)
	}

	if err := validate.StepDefinition(step); err != nil {
		errorMessage := err.Error()
		switch {
		case errorMessage == "summary should be one line":
			log.Warnf(" " + errorMessage)
		case strings.Contains(errorMessage, "summary should contain maximum"):
			log.Warnf(" " + errorMessage)
		default:
			return err
		}
	}

	log.Donef("Success")
	return nil
}

var auditStepLibraryCommand = cli.Command{
	Name:  "audit-step-lib",
	Usage: "Validates a StepLibrary: checks every step's id, version, definition. Optionally checks step's publish params, type-tags and project-type-tags",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "uri",
			Usage: "The StepLibrary repository's git URI.",
		},
		cli.StringFlag{
			Name:  "steps",
			Usage: "Comma separated list of steps to ensure publish params (format: id@version / id [to check every versions])",
		},
		cli.StringFlag{
			Name:  "project-type-tags",
			Usage: "Comma separated list of allowed Project Type Tags",
		},
		cli.StringFlag{
			Name:  "type-tags",
			Usage: "Comma separated list of allowed Type Tags",
		},
		cli.BoolFlag{
			Name:  "latest-only",
			Usage: "Check tags (project-type-tags, type-tags) on latest step versions only",
		},
	},
	Action: func(c *cli.Context) error {
		if err := auditStepLibrary(c); err != nil {
			logrus.Fatal(err)
		}
		return nil
	},
}

func splitListAndRemoveEmptyItems(list, sep string) []string {
	split := strings.Split(list, sep)
	trimmed := []string{}
	for _, str := range split {
		if str != "" {
			trimmed = append(trimmed, str)
		}
	}
	return trimmed
}

func firstUniqItem(slice []string, sub []string) string {
	for _, subItem := range sub {
		allowed := false
		for _, item := range slice {
			if item == subItem {
				allowed = true
				break
			}
		}
		if !allowed {
			return subItem
		}
	}
	return ""
}

func auditStepLibrary(c *cli.Context) error {
	uri := c.String("uri")
	stepListParam := c.String("steps")
	projectTypeTagsParam := c.String("project-type-tags")
	typeTagsParam := c.String("type-tags")
	latestOnly := c.Bool("latest-only")

	fmt.Println()
	log.Infof("Audting StepLib:")
	log.Printf("- StepLib repository uri: %s", uri)
	log.Printf("- Check publish params for Steps: %v", stepListParam)
	log.Printf("- Allowed project-type-tags: %v", projectTypeTagsParam)
	log.Printf("- Allowed type-tags: %v", typeTagsParam)
	fmt.Println()

	if uri == "" {
		return errors.New("steplib uri not specified")
	}

	projectTypeTags := splitListAndRemoveEmptyItems(projectTypeTagsParam, ",")
	typeTags := splitListAndRemoveEmptyItems(typeTagsParam, ",")
	stepsParam := splitListAndRemoveEmptyItems(stepListParam, ",")

	if exist, err := stepman.RootExistForLibrary(uri); err != nil {
		return err
	} else if !exist {
		return fmt.Errorf("missing routing for StepLib, call 'stepman setup -c %s' before audit", uri)
	}

	stepLib, err := stepman.ReadStepSpec(uri)
	if err != nil {
		return err
	}

	// create stepID - stepVersions mapping
	// stepVersions: list of versions, or []string{"*"} to check all versions
	stepIDVersionsMap := map[string][]string{}
	for _, stepParam := range stepsParam {
		split := splitListAndRemoveEmptyItems(stepParam, "@")
		id := split[0]

		if err := validate.StepParamID(id); err != nil {
			return fmt.Errorf("invalid step param (%s), error: %s", stepParam, err)
		}

		if len(split) == 2 {
			version := split[1]

			if err := validate.StepParamVersion(version); err != nil {
				return fmt.Errorf("invalid step param (%s), error: %s", stepParam, err)
			}

			if !stepLib.IsStepExist(id, version) {
				return fmt.Errorf("step %s version %s not exist in the StepLib", id, version)
			}

			versions, found := stepIDVersionsMap[id]
			if !found {
				versions = []string{}
			}

			if len(versions) == 1 && versions[0] == "*" {
				log.Warnf("step (%s) defined without version and with version (%s), auditing every version", id, version)
			} else {
				versions = append(versions, version)
			}

			stepIDVersionsMap[id] = versions
		} else {
			if !stepLib.IsStepExist(id, "") {
				return fmt.Errorf("step %s not exist in the StepLib", id)
			}

			versions, found := stepIDVersionsMap[id]
			if !found {
				versions = []string{}
			}

			if len(versions) > 0 && versions[0] != "*" {
				log.Warnf("step (%s) defined without version and with version (%s), auditing every version", id, versions[0])
			}

			stepIDVersionsMap[id] = []string{"*"}
		}
	}
	// ---

	for stepID, stepGroup := range stepLib.Steps {
		fmt.Println()
		log.Infof("Auditing step group: %s", stepID)

		if err := validate.StepParamID(stepID); err != nil {
			return err
		}

		versionsToCheck, found := stepIDVersionsMap[stepID]
		if !found {
			versionsToCheck = []string{}
		}

		for stepVersion, step := range stepGroup.Versions {
			log.Printf(" version: %s", stepVersion)

			if err := validate.StepParamVersion(stepVersion); err != nil {
				return err
			}

			if err := validate.StepDefinition(step); err != nil {
				errorMessage := err.Error()
				switch {
				case errorMessage == "summary should be one line":
					log.Warnf(" " + errorMessage)
				case strings.Contains(errorMessage, "summary should contain maximum"):
					log.Warnf(" " + errorMessage)
				default:
					return err
				}
			}

			for _, versionToCheck := range versionsToCheck {
				if versionToCheck == "*" || versionToCheck == stepVersion {
					log.Printf(" ensure publish params")

					if err := validate.StepDefinitionPublishParams(step, stepID, stepVersion); err != nil {
						return err
					}
					break
				}
			}

			if !latestOnly {
				if len(projectTypeTags) > 0 {
					log.Printf(" ensure project-type-tags")

					uniq := firstUniqItem(projectTypeTags, step.ProjectTypeTags)
					if uniq != "" {
						return fmt.Errorf("project-type-tag (%s) is not allowed", uniq)
					}
				}

				if len(typeTags) > 0 {
					log.Printf(" ensure type-tags")

					uniq := firstUniqItem(typeTags, step.TypeTags)
					if uniq != "" {
						return fmt.Errorf("type-tag (%s) is not allowed", uniq)
					}
				}
			}
		}

		if latestOnly {
			step, found := stepGroup.LatestVersion()
			if !found {
				log.Warnf("%s latest version not found", stepID)
			} else {
				if len(projectTypeTags) > 0 {
					log.Printf(" ensure project-type-tags")

					uniq := firstUniqItem(projectTypeTags, step.ProjectTypeTags)
					if uniq != "" {
						return fmt.Errorf("project-type-tag (%s) is not allowed", uniq)
					}
				}

				if len(typeTags) > 0 {
					log.Printf(" ensure type-tags")

					uniq := firstUniqItem(typeTags, step.TypeTags)
					if uniq != "" {
						return fmt.Errorf("type-tag (%s) is not allowed", uniq)
					}
				}
			}
		}

		log.Donef("Success")
	}

	return nil
}

//
// [DEPRECATED] Audit Command

func auditStepBeforeShare(pth string) error {
	stepModel, err := stepman.ParseStepDefinition(pth, false)
	if err != nil {
		return err
	}
	return stepModel.AuditBeforeShare()
}

func detectStepIDAndVersionFromPath(pth string) (stepID, stepVersion string, err error) {
	pathComps := strings.Split(pth, "/")
	if len(pathComps) < 4 {
		err = fmt.Errorf("Path should contain at least 4 components: steps, step-id, step-version, step.yml: %s", pth)
		return
	}
	// we only care about the last 4 component of the path
	pathComps = pathComps[len(pathComps)-4:]
	if pathComps[0] != "steps" {
		err = fmt.Errorf("Invalid step.yml path, 'steps' should be included right before the step-id: %s", pth)
		return
	}
	if pathComps[3] != "step.yml" {
		err = fmt.Errorf("Invalid step.yml path, should end with 'step.yml': %s", pth)
		return
	}
	stepID = pathComps[1]
	stepVersion = pathComps[2]
	return
}

func auditStepBeforeSharePullRequest(pth string) error {
	stepID, version, err := detectStepIDAndVersionFromPath(pth)
	if err != nil {
		return err
	}

	stepModel, err := stepman.ParseStepDefinition(pth, false)
	if err != nil {
		return err
	}

	return auditStepModelBeforeSharePullRequest(stepModel, stepID, version)
}

func auditStepModelBeforeSharePullRequest(step models.StepModel, stepID, version string) error {
	if err := step.Audit(); err != nil {
		return fmt.Errorf("Failed to audit step infos, error: %s", err)
	}

	pth, err := pathutil.NormalizedOSTempDirPath(stepID + version)
	if err != nil {
		return fmt.Errorf("Failed to create a temporary directory for the step's audit, error: %s", err)
	}

	if step.Source == nil {
		return fmt.Errorf("Missing Source porperty")
	}

	err = retry.Times(2).Wait(3 * time.Second).Try(func(attempt uint) error {
		return git.CloneTagOrBranch(step.Source.Git, pth, version)
	})
	if err != nil {
		return fmt.Errorf("Failed to git-clone the step (url: %s) version (%s), error: %s",
			step.Source.Git, version, err)
	}

	latestCommit, err := git.GetCommitHashOfHead(pth)
	if err != nil {
		return fmt.Errorf("Failed to get git-latest-commit-hash, error: %s", err)
	}
	if latestCommit != step.Source.Commit {
		return fmt.Errorf("Step commit hash (%s) should be the latest commit hash (%s) on git tag", step.Source.Commit, latestCommit)
	}

	return nil
}

func auditStepLibBeforeSharePullRequest(gitURI string) error {
	if exist, err := stepman.RootExistForLibrary(gitURI); err != nil {
		return err
	} else if !exist {
		return fmt.Errorf("Missing routing for collection, call 'stepman setup -c %s' before audit", gitURI)
	}

	collection, err := stepman.ReadStepSpec(gitURI)
	if err != nil {
		return err
	}

	for stepID, stepGroup := range collection.Steps {
		logrus.Debugf("Start audit StepGrup, with ID: (%s)", stepID)
		for version, step := range stepGroup.Versions {
			logrus.Debugf("Start audit Step (%s) (%s)", stepID, version)
			if err := auditStepModelBeforeSharePullRequest(step, stepID, version); err != nil {
				logrus.Errorf(" * "+colorstring.Redf("[FAILED] ")+"Failed audit (%s) (%s)", stepID, version)
				return fmt.Errorf("   Error: %s", err.Error())
			}
			logrus.Infof(" * "+colorstring.Greenf("[OK] ")+"Success audit (%s) (%s)", stepID, version)
		}
	}
	return nil
}

var auditCommand = cli.Command{
	Name:   "audit",
	Usage:  "[DEPRECATED] Use audit-step or audit-step-library commands. Validates Step or Step Collection.",
	Action: audit,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   CollectionKey + ", " + collectionKeyShort,
			Usage:  "For validating Step Collection before share.",
			EnvVar: CollectionPathEnvKey,
		},
		cli.StringFlag{
			Name:  "step-yml",
			Usage: "For validating Step before share or before share Pull Request.",
		},
		cli.BoolFlag{
			Name:  "before-pr",
			Usage: "If flag is set, Step Pull Request required fields will be checked to. Note: only for Step audit.",
		},
	},
}

func audit(c *cli.Context) error {
	// Input validation
	beforePR := c.Bool("before-pr")

	collectionURI := c.String("collection")
	if collectionURI != "" {
		if beforePR {
			logrus.Warnln("before-pr flag is used only for Step audit")
		}

		if err := auditStepLibBeforeSharePullRequest(collectionURI); err != nil {
			logrus.Fatalf("Audit Step Collection failed, err: %s", err)
		}
	} else {
		stepYMLPath := c.String("step-yml")
		if stepYMLPath != "" {
			if exist, err := pathutil.IsPathExists(stepYMLPath); err != nil {
				logrus.Fatalf("Failed to check path (%s), err: %s", stepYMLPath, err)
			} else if !exist {
				logrus.Fatalf("step.yml doesn't exist at: %s", stepYMLPath)
			}

			if beforePR {
				if err := auditStepBeforeSharePullRequest(stepYMLPath); err != nil {
					logrus.Fatalf("Step audit failed, err: %s", err)
				}
			} else {
				if err := auditStepBeforeShare(stepYMLPath); err != nil {
					logrus.Fatalf("Step audit failed, err: %s", err)
				}
			}

			logrus.Infof(" * "+colorstring.Greenf("[OK] ")+"Success audit (%s)", stepYMLPath)
		} else {
			logrus.Fatalln("'stepman audit' command needs --collection or --step-yml flag")
		}
	}

	return nil
}
