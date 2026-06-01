// Command steplib-gen reads a bitrise-steplib clone and writes the V2 inventory
// tree to an output directory.
//
//	steplib-gen -input <bitrise-steplib-clone> -output <out-dir> [-commit-sha <sha>]
//
// See STEP-2374-plan.md and docs/spec-v2/ for the format being generated.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/bitrise-io/stepman/steplibrary/specgen"
)

type stderrLogger struct{}

func (stderrLogger) Debugf(format string, v ...any) {} // suppress; turn on with -verbose if needed
func (stderrLogger) Infof(format string, v ...any)  { fmt.Fprintf(os.Stderr, format+"\n", v...) }
func (stderrLogger) Warnf(format string, v ...any) {
	fmt.Fprintf(os.Stderr, "warn: "+format+"\n", v...)
}
func (stderrLogger) Errorf(format string, v ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", v...)
}

func main() {
	var (
		input     = flag.String("input", "", "path to a bitrise-steplib clone (required)")
		output    = flag.String("output", "", "output directory for the V2 tree (required)")
		commitSHA = flag.String("commit-sha", "", "optional steplib commit sha to record in meta.json")
		timestamp = flag.String("timestamp", "", "RFC3339 timestamp to record as updated_at (default: time.Now UTC). Set for reproducible output (e.g., sample-output regeneration).")
	)
	flag.Parse()

	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "both -input and -output are required")
		flag.Usage()
		os.Exit(2)
	}

	generatedAt := time.Now().UTC()
	if *timestamp != "" {
		ts, err := time.Parse(time.RFC3339, *timestamp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid -timestamp: %s\n", err)
			os.Exit(2)
		}
		generatedAt = ts
	}

	opts := specgen.Options{
		GeneratedAt:      generatedAt,
		SteplibCommitSHA: *commitSHA,
	}

	stats, err := specgen.GenerateFromSteplibClone(os.DirFS(*input), *output, opts, stderrLogger{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr,
		"\nGenerated V2 inventory:\n"+
			"  output:   %s\n"+
			"  steps:    %d\n"+
			"  versions: %d\n"+
			"  files:    %d\n"+
			"  bytes:    %d (%.2f MB)\n"+
			"  duration: %s\n",
		*output, stats.StepCount, stats.VersionCount, stats.FilesWritten,
		stats.BytesWritten, float64(stats.BytesWritten)/1024/1024, stats.Duration.Round(time.Millisecond),
	)
}
