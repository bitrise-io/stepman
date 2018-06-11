package cli

import (
	"fmt"

	"github.com/bitrise-io/go-utils/colorstring"
	"github.com/bitrise-io/go-utils/command/git"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/stepman/stepman"
	"github.com/urfave/cli"
)

func printFinishShare() {
	fmt.Println()
	log.Printf(" * " + colorstring.Green("[OK] ") + "Yeah!! You rock!!")
	fmt.Println()
	fmt.Println("   " + GuideTextForFinish())
	fmt.Println()
	msg := `   You can create a pull request in your forked StepLib repository,
   if you used the main StepLib repository then your repository's url looks like: ` + `
   ` + colorstring.Green("https://github.com/[your-username]/bitrise-steplib") + `

   On GitHub you can find a ` + colorstring.Green("'Compare & pull request'") + ` button, in the ` + colorstring.Green("'Your recently pushed branches:'") + ` section,
   which will bring you to the 'Open a pull request' page, where you can review and create your Pull Request.
	`
	fmt.Println(msg)
}

func finish(c *cli.Context) error {
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

	repo, err := git.New(collectionDir)
	if err != nil {
		fail(err.Error())
	}

	out, err := repo.Status("--porcelain").RunAndReturnTrimmedCombinedOutput()
	if err != nil {
		fail(err.Error())
	}
	if out == "" {
		log.Warnf("No git changes!")
		printFinishShare()
		return nil
	}

	stepDirInSteplib := stepman.GetStepCollectionDirPath(route, share.StepID, share.StepTag)
	stepYMLPathInSteplib := stepDirInSteplib + "/step.yml"
	log.Infof("New step.yml:", stepYMLPathInSteplib)
	if err := repo.Add(stepYMLPathInSteplib).Run(); err != nil {
		fail(err.Error())
	}

	log.Infof("Do commit")
	msg := share.StepID + " " + share.StepTag
	if err := repo.Commit(msg).Run(); err != nil {
		fail(err.Error())
	}

	log.Infof("Pushing to your fork: ", share.Collection)
	if err := repo.Push(share.ShareBranchName()).Run(); err != nil {
		fail(err.Error())
	}
	printFinishShare()

	return nil
}
