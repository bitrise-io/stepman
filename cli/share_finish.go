package cli

import (
	"fmt"

	"github.com/bitrise-io/go-utils/command/git"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/stepman/stepman"
	"github.com/bitrise-io/stepman/stringbuilder"
	"github.com/urfave/cli"
)

func printFinishShare() {
	b := stringbuilder.New()
	b.Add(GuideTextForFinish())
	b.AddNewLine()
	b.AddLn("On GitHub you can find a ").AddBlue("Compare & pull request").Add(" button, in the section called ").AddBlue("Your recently pushed branches:").Add(",")
	b.AddLn("which will bring you to the page to ").AddBlue("Open a pull request").Add(", where you can review and create your Pull Request.")
	fmt.Println(b.String())
}

func finish(c *cli.Context) error {
	log.Infof("Validating Step share params...")

	share, err := ReadShareSteplibFromFile()
	if err != nil {
		log.Errorf(err.Error())
		fail("You have to start sharing with `stepman share start`, or you can read instructions with `stepman share`")
	}

	route, found := stepman.ReadRoute(share.Collection)
	if !found {
		fail("No route found for collectionURI (%s)", share.Collection)
	}

	collectionDir := stepman.GetLibraryBaseDirPath(route)
	log.Donef("all inputs are valid")

	fmt.Println()
	log.Infof("Checking StepLib changes...")
	repo, err := git.New(collectionDir)
	if err != nil {
		fail(err.Error())
	}

	out, err := repo.Status("--porcelain").RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		fail(err.Error())
	}
	if out == "" {
		log.Warnf("No git changes, it seems you already called this command")
		fmt.Println()
		printFinishShare()
		return nil
	}

	stepDirInSteplib := stepman.GetStepCollectionDirPath(route, share.StepID, share.StepTag)
	stepYMLPathInSteplib := stepDirInSteplib + "/step.yml"
	log.Printf("new step.yml: %s", stepYMLPathInSteplib)
	if err := repo.Add(stepYMLPathInSteplib).Run(); err != nil {
		fail(err.Error())
	}

	fmt.Println()
	log.Infof("Submitting the changes...")
	msg := share.StepID + " " + share.StepTag
	if err := repo.Commit(msg).Run(); err != nil {
		fail(err.Error())
	}

	log.Printf("pushing to your fork: %s", share.Collection)
	if out, err := repo.Push(share.ShareBranchName()).RunAndReturnTrimmedCombinedOutput(); err != nil {
		fail(out)
	}

	fmt.Println()
	printFinishShare()
	fmt.Println()

	return nil
}
