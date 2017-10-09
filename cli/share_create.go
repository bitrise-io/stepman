package cli

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	log "github.com/Sirupsen/logrus"
	"github.com/bitrise-io/go-utils/colorstring"
	"github.com/bitrise-io/go-utils/command/git"
	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-utils/pointers"
	"github.com/bitrise-io/go-utils/retry"
	"github.com/bitrise-io/goinp/goinp"
	"github.com/bitrise-io/stepman/models"
	"github.com/bitrise-io/stepman/stepman"
	"github.com/bitrise-io/stepman/validate"
	"github.com/urfave/cli"
)

const maxSummaryLength = 100

func printFinishCreate(share ShareModel, stepDirInSteplib string, toolMode bool) {
	fmt.Println()
	log.Infof(" * "+colorstring.Green("[OK]")+" Your Step (%s) (%s) added to local StepLib (%s).", share.StepID, share.StepTag, stepDirInSteplib)
	log.Infoln(" *      You can find your Step's step.yml at: " + colorstring.Greenf("%s/step.yml", stepDirInSteplib))
	fmt.Println()
	fmt.Println("   " + GuideTextForShareFinish(toolMode))
}

func getStepIDFromGit(git string) string {
	splits := strings.Split(git, "/")
	lastPart := splits[len(splits)-1]
	splits = strings.Split(lastPart, ".")
	return splits[0]
}

func create(c *cli.Context) error {
	toolMode := c.Bool(ToolMode)

	share, err := ReadShareSteplibFromFile()
	if err != nil {
		log.Error(err)
		log.Fatalln("You have to start sharing with `stepman share start`, or you can read instructions with `stepman share`")
	}

	// Input validation
	tag := c.String(TagKey)
	gitURI := c.String(GitKey)
	stepID := c.String(StepIDKEy)
	if stepID == "" {
		stepID = getStepIDFromGit(gitURI)
	}

	if err := validate.StepParams(gitURI, stepID, tag); err != nil {
		log.Fatal(err.Error())
	}

	if err := validate.IfStepNotExistInSteplib(stepID, tag, share.Collection); err != nil {
		if err.Error() == "step version already exist" {
			log.Warn(err.Error())
			if val, err := goinp.AskForBool("Would you like to overwrite local version of Step?"); err != nil {
				log.Fatalf("Failed to get bool, err: %s", err)
			} else {
				if !val {
					log.Errorln("Unfortunately we can't continue with sharing without an overwrite exist step.yml.")
					log.Fatalln("Please finish your changes, run this command again and allow it to overwrite the exist step.yml!")
				}
			}
		}
	}

	// Clone Step to tmp dir
	tmp, err := pathutil.NormalizedOSTempDirPath("")
	if err != nil {
		log.Fatalf("Failed to get temp directory, err: %s", err)
	}

	log.Infof("Cloning Step from (%s) with tag (%s) to temporary path (%s)", gitURI, tag, tmp)
	if err := retry.Times(2).Wait(3 * time.Second).Try(func(attempt uint) error {
		return git.CloneTagOrBranch(gitURI, tmp, tag)
	}); err != nil {
		log.Fatalf("Failed to git-clone (url: %s) version (%s), error: %s",
			gitURI, tag, err)
	}

	if err := validate.StepRepo(tmp); err != nil {
		log.Fatal(err)
	}

	// Update step.yml
	tmpStepYMLPath := path.Join(tmp, "step.yml")
	bytes, err := fileutil.ReadBytesFromFile(tmpStepYMLPath)
	if err != nil {
		log.Fatalf("Failed to read Step from file, err: %s", err)
	}
	var stepModel models.StepModel
	if err := yaml.Unmarshal(bytes, &stepModel); err != nil {
		log.Fatalf("Failed to unmarchal Step, err: %s", err)
	}

	commit, err := git.GetCommitHashOfHead(tmp)
	if err != nil {
		log.Fatalf("Failed to get commit hash, err: %s", err)
	}
	stepModel.Source = &models.StepSourceModel{
		Git:    gitURI,
		Commit: commit,
	}
	stepModel.PublishedAt = pointers.NewTimePtr(time.Now())

	if err := validate.StepDefinition(stepModel); err != nil {
		errorMessage := err.Error()
		switch {
		case errorMessage == "summary should be one line":
			log.Warnf(" " + errorMessage)
		case strings.Contains(errorMessage, "summary should contain maximum"):
			log.Warnf(" " + errorMessage)
		default:
			log.Fatal(err.Error())
		}
	}

	// Copy step.yml to steplib
	share.StepID = stepID
	share.StepTag = tag
	if err := WriteShareSteplibToFile(share); err != nil {
		log.Fatalf("Failed to save share steplib to file, err: %s", err)
	}

	route, found := stepman.ReadRoute(share.Collection)
	if !found {
		log.Fatalf("No route found for collectionURI (%s)", share.Collection)
	}
	stepDirInSteplib := stepman.GetStepCollectionDirPath(route, stepID, tag)
	stepYMLPathInSteplib := path.Join(stepDirInSteplib, "step.yml")

	log.Info("Step dir in collection:", stepDirInSteplib)
	if exist, err := pathutil.IsPathExists(stepDirInSteplib); err != nil {
		log.Fatalf("Failed to check path (%s), err: %s", stepDirInSteplib, err)
	} else if !exist {
		if err := os.MkdirAll(stepDirInSteplib, 0777); err != nil {
			log.Fatalf("Failed to create path (%s), err: %s", stepDirInSteplib, err)
		}
	}

	log.Infof("Checkout branch: %s", share.ShareBranchName())
	collectionDir := stepman.GetLibraryBaseDirPath(route)
	if err := git.Checkout(collectionDir, share.ShareBranchName()); err != nil {
		if err := git.CreateAndCheckoutBranch(collectionDir, share.ShareBranchName()); err != nil {
			log.Fatalf("Git failed to create and checkout branch, err: %s", err)
		}
	}

	stepBytes, err := yaml.Marshal(stepModel)
	if err != nil {
		log.Fatalf("Failed to marcshal Step model, err: %s", err)
	}
	if err := fileutil.WriteBytesToFile(stepYMLPathInSteplib, stepBytes); err != nil {
		log.Fatalf("Failed to write Step to file, err: %s", err)
	}

	// Update spec.json
	if err := stepman.ReGenerateLibrarySpec(route); err != nil {
		log.Fatalf("Failed to re-create steplib, err: %s", err)
	}

	printFinishCreate(share, stepDirInSteplib, toolMode)

	return nil
}
