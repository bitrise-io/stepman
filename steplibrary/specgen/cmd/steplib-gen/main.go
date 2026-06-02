// Command steplib-gen sets up a step library by its source URI (cloning it into
// stepman's local cache if needed) and writes the V2 inventory tree to an output
// directory.
//
//	steplib-gen -steplib <steplib-source-uri> -output <out-dir> [-commit-sha <sha>]
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
		steplib   = flag.String("steplib", "", "steplib source URI to set up and generate from (required)")
		output    = flag.String("output", "", "output directory for the V2 tree (required)")
		commitSHA = flag.String("commit-sha", "", "optional steplib commit sha to record in meta.json")
		timestamp = flag.String("timestamp", "", "RFC3339 timestamp to record as updated_at (default: time.Now UTC). Set for reproducible output (e.g., sample-output regeneration).")
	)
	flag.Parse()

	if *steplib == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "both -steplib and -output are required")
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

	stats, err := specgen.Generate(*steplib, *output, opts, stderrLogger{})
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
