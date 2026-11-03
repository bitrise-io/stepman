package integration

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

// Golden parity suite: every case runs the binary under test (INTEGRATION_TEST_BINARY_PATH) in a hermetic
// work dir and HOME, and records exit code, stdout, stderr and every file change per step into testdata/golden/<case>.yml.
// Regenerate with: go test ./_tests/integration/ -run TestGolden -update
var updateGolden = flag.Bool("update", false, "update golden files")

const (
	goldenDir          = "testdata/golden"
	workPlaceholder    = "$WORK"
	steplibPlaceholder = "$STEPLIB"
	fixtureStepID      = "test-step"
	fixtureStepVersion = "1.0.0"
	fixtureGitName     = "Stepman Golden"
	fixtureGitEmail    = "stepman-golden@bitrise.io"
	fixtureGitDate     = "2020-01-01T00:00:00Z"
	maxInlineContent   = 1024
)

type goldenStep struct {
	args  []string
	env   []string
	stdin *string
}

type goldenCase struct {
	name string
	// needsSteplib copies testdata/fixture-steplib into $WORK/steplib and
	// turns it into a git repo with a fixed identity. $STEPLIB in args and env
	// resolves to that path.
	needsSteplib bool
	files        map[string]string
	env          []string
	steps        []goldenStep
}

type goldenFile struct {
	Steps []goldenResult `yaml:"steps"`
}

type goldenResult struct {
	Args         []string          `yaml:"args,flow"`
	Env          []string          `yaml:"env,omitempty"`
	Stdin        *string           `yaml:"stdin,omitempty"`
	ExitCode     int               `yaml:"exit_code"`
	Stdout       string            `yaml:"stdout,omitempty"`
	Stderr       string            `yaml:"stderr,omitempty"`
	ChangedFiles map[string]string `yaml:"changed_files,omitempty"`
	RemovedFiles []string          `yaml:"removed_files,omitempty"`
}

func stepmanCmd(args ...string) goldenStep {
	return goldenStep{args: args}
}

func (s goldenStep) withStdin(stdin string) goldenStep {
	s.stdin = &stdin
	return s
}

func (s goldenStep) withEnv(env ...string) goldenStep {
	s.env = append(s.env, env...)
	return s
}

func TestGolden(t *testing.T) {
	bin, err := exec.LookPath(binPath())
	require.NoError(t, err)
	bin, err = filepath.Abs(bin)
	require.NoError(t, err)

	version, err := exec.Command(bin, "--version").Output()
	require.NoError(t, err)
	t.Logf("binary under test: %s (%s)", bin, strings.TrimSpace(string(version)))

	names := map[string]bool{}
	for _, tc := range goldenCases {
		require.False(t, names[tc.name], "duplicate case name: %s", tc.name)
		names[tc.name] = true

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := runGoldenCase(t, bin, strings.TrimSpace(string(version)), tc)
			pth := filepath.Join(goldenDir, tc.name+".golden")
			if *updateGolden {
				content, err := yaml.Marshal(goldenFile{Steps: got})
				require.NoError(t, err)
				require.NoError(t, os.MkdirAll(goldenDir, 0755))
				require.NoError(t, os.WriteFile(pth, content, 0644))
				return
			}

			content, err := os.ReadFile(pth)
			require.NoError(t, err, "missing golden file, run with -update")
			var want goldenFile
			require.NoError(t, yaml.Unmarshal(content, &want), pth)
			compareSteps(t, pth, want.Steps, got)
		})
	}
}

func compareSteps(t *testing.T, goldenPth string, want, got []goldenResult) {
	for i := 0; i < len(want) || i < len(got); i++ {
		var w, g goldenResult
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		wantYML, gotYML := mustMarshal(t, w), mustMarshal(t, g)
		if wantYML == gotYML {
			continue
		}

		args := g.Args
		if i >= len(got) {
			args = w.Args
		}
		diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(wantYML),
			B:        difflib.SplitLines(gotYML),
			FromFile: "want (" + goldenPth + ")",
			ToFile:   "got",
			Context:  3,
		})
		t.Errorf("step %d/%d differs: stepman %s\n%s", i+1, len(want), strings.Join(args, " "), diff)
	}
}

func mustMarshal(t *testing.T, v interface{}) string {
	b, err := yaml.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func runGoldenCase(t *testing.T, bin, version string, tc goldenCase) []goldenResult {
	work := t.TempDir()
	realWork, err := filepath.EvalSymlinks(work)
	require.NoError(t, err)
	home := filepath.Join(work, "home")
	require.NoError(t, os.MkdirAll(home, 0755))

	for name, content := range tc.files {
		pth := filepath.Join(work, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(pth), 0755))
		require.NoError(t, os.WriteFile(pth, []byte(content), 0644))
	}

	steplib := ""
	if tc.needsSteplib {
		steplib = materializeSteplib(t, work)
	}

	n := newNormalizer(work, realWork, steplib, version)
	resolve := func(s string) string {
		s = strings.ReplaceAll(s, workPlaceholder, work)
		if steplib != "" {
			s = strings.ReplaceAll(s, steplibPlaceholder, steplib)
		}
		return s
	}

	var results []goldenResult
	prev := snapshotFiles(t, work)
	for _, step := range tc.steps {
		env := append(append([]string{}, tc.env...), step.env...)
		args := make([]string, len(step.args))
		for i, a := range step.args {
			args[i] = resolve(a)
		}

		cmd := exec.Command(bin, args...)
		cmd.Args[0] = "stepman"
		cmd.Dir = work
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}
		for _, e := range env {
			cmd.Env = append(cmd.Env, resolve(e))
		}
		if step.stdin != nil {
			cmd.Stdin = strings.NewReader(*step.stdin)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		exitCode := 0
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			require.True(t, errors.As(err, &exitErr), "failed to run command: %s", err)
			exitCode = exitErr.ExitCode()
		}

		stdoutStr := n.normalize(trimTrailingSpaces(stdout.String()))
		if isPrintCommand(step.args) {
			stdoutStr = sortLines(stdoutStr)
		}

		result := goldenResult{
			Args:     append([]string{}, step.args...),
			Env:      env,
			Stdin:    step.stdin,
			ExitCode: exitCode,
			Stdout:   stdoutStr,
			Stderr:   n.normalize(trimTrailingSpaces(stderr.String())),
		}

		curr := snapshotFiles(t, work)
		// Diff on normalized content so epoch rewrites of e.g. spec.json
		// (collapsed to $GENERATED_AT) do not surface as changed_files noise.
		normPrev := make(map[string]string, len(prev))
		for name, content := range prev {
			normPrev[n.normalize(name)] = n.normalize(content)
		}
		normCurr := make(map[string]string, len(curr))
		for name, content := range curr {
			normCurr[n.normalize(name)] = n.normalize(content)
		}
		for _, name := range sortedKeys(normCurr) {
			if prevContent, ok := normPrev[name]; !ok || prevContent != normCurr[name] {
				if result.ChangedFiles == nil {
					result.ChangedFiles = map[string]string{}
				}
				result.ChangedFiles[name] = fileSummary(normCurr[name])
			}
		}
		for _, name := range sortedKeys(normPrev) {
			if _, ok := normCurr[name]; !ok {
				result.RemovedFiles = append(result.RemovedFiles, name)
			}
		}
		prev = curr
		results = append(results, result)
	}

	return results
}

type normalizer struct {
	work, realWork, steplib, version string
	versionRe                        *regexp.Regexp
}

func newNormalizer(work, realWork, steplib, version string) normalizer {
	n := normalizer{work: work, realWork: realWork, steplib: steplib, version: version}
	if version != "" {
		n.versionRe = regexp.MustCompile(`\b` + regexp.QuoteMeta(version) + `\b`)
	}
	return n
}

var (
	ansiPattern         = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	logrusTimePattern   = regexp.MustCompile(`(?m)^([A-Z]{4})\[\d{2}:\d{2}:\d{2}\]`)
	stdlogPrefixPattern = regexp.MustCompile(`(?m)^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)
	logrusTextTime      = regexp.MustCompile(`time="[^"]*"`)
	stackTracePattern   = regexp.MustCompile(`(?s)\ngoroutine \d+ \[.*`)
	sigsegvPattern      = regexp.MustCompile(`code=0x[0-9a-f]+ addr=0x[0-9a-f]+ pc=0x[0-9a-f]+`)
	// step_collections/<unix-timestamp>/ — the folder alias stepman mints per route.
	stepCollectionAliasPattern = regexp.MustCompile(`step_collections/\d+`)
	// routing.json stores the alias as a bare value. Over-eager on purpose; audit when
	// adding cases that emit unrelated 10+-digit quoted numeric strings.
	routingAliasPattern = regexp.MustCompile(`"\d{10,}"`)
	// generated_at_timestamp: <epoch> in the JSON spec and slim-spec.
	generatedAtPattern = regexp.MustCompile(`"generated_at_timestamp":\s*\d+`)
	// published_at: "<iso-ts>" injected by share create.
	publishedAtJSONPattern = regexp.MustCompile(`"published_at":\s*"[^"]*"`)
	publishedAtYAMLPattern = regexp.MustCompile(`published_at:\s*\S+`)
	// source.commit / commit: 40-char hex (SHA1) from `git rev-parse` on the fixture.
	// Over-eager on purpose; audit when adding cases that emit unrelated 40-hex tokens.
	sha1Pattern    = regexp.MustCompile(`\b[0-9a-f]{40}\b`)
	tmpStepPattern = regexp.MustCompile(`(?:/private)?/(?:tmp|var/folders/[^/]+/[^/]+/T)/[^/\s]*` + regexp.QuoteMeta(fixtureStepID+fixtureStepVersion) + `[^/\s]*(?:/[^\s]*)?`)
)

func (n normalizer) normalize(s string) string {
	s = stackTracePattern.ReplaceAllString(s, "\n<stack trace>\n")
	s = sigsegvPattern.ReplaceAllString(s, "code=<code> addr=<addr> pc=<pc>")
	s = logrusTextTime.ReplaceAllString(s, `time="<time>"`)
	s = ansiPattern.ReplaceAllString(s, "")
	s = logrusTimePattern.ReplaceAllString(s, "$1[HH:MM:SS]")
	s = stdlogPrefixPattern.ReplaceAllString(s, "YYYY/MM/DD HH:MM:SS ")
	s = tmpStepPattern.ReplaceAllString(s, "$$TMP_STEP")
	if n.steplib != "" {
		s = strings.ReplaceAll(s, n.steplib, steplibPlaceholder)
	}
	s = strings.ReplaceAll(s, n.realWork, workPlaceholder)
	s = strings.ReplaceAll(s, n.work, workPlaceholder)
	s = stepCollectionAliasPattern.ReplaceAllString(s, "step_collections/$$ALIAS")
	s = routingAliasPattern.ReplaceAllString(s, `"$$ALIAS"`)
	s = generatedAtPattern.ReplaceAllString(s, `"generated_at_timestamp": $$GENERATED_AT`)
	s = publishedAtJSONPattern.ReplaceAllString(s, `"published_at": "$$PUBLISHED_AT"`)
	s = publishedAtYAMLPattern.ReplaceAllString(s, "published_at: $$PUBLISHED_AT")
	s = sha1Pattern.ReplaceAllString(s, "$$SHA")
	if n.versionRe != nil {
		s = n.versionRe.ReplaceAllString(s, "$$VERSION")
	}
	return s
}

// materializeSteplib copies the fixture steplib into $WORK/steplib, inits a git repo
// with a fixed identity and committer/author dates, and returns the absolute path.
// Needed by cases that drive `share start` (always clones) or audit flows that
// expect source.commit to resolve to something.
func materializeSteplib(t *testing.T, workDir string) string {
	t.Helper()
	dst := filepath.Join(workDir, "steplib")
	cmd := exec.Command("cp", "-R", "testdata/fixture-steplib", dst)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "cp fixture-steplib: %s", out)

	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME="+fixtureGitName,
		"GIT_AUTHOR_EMAIL="+fixtureGitEmail,
		"GIT_AUTHOR_DATE="+fixtureGitDate,
		"GIT_COMMITTER_NAME="+fixtureGitName,
		"GIT_COMMITTER_EMAIL="+fixtureGitEmail,
		"GIT_COMMITTER_DATE="+fixtureGitDate,
	)
	runGit := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dst
		c.Env = gitEnv
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	runGit("config", "commit.gpgsign", "false")
	runGit("config", "tag.gpgsign", "false")
	runGit("config", "user.name", fixtureGitName)
	runGit("config", "user.email", fixtureGitEmail)
	runGit("add", "-A")
	runGit("commit", "-q", "-m", "fixture steplib")

	return dst
}

// isPrintCommand returns true for subcommands whose human-readable output
// iterates maps (step-list, collections) and would otherwise jitter line order
// between runs. The emitted lines go through sortLines before comparison.
func isPrintCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "step-list", "collections":
		return true
	}
	return false
}

// sortLines stabilises the output of commands that iterate a map.
func sortLines(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func trimTrailingSpaces(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

func snapshotFiles(t *testing.T, root string) map[string]string {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(pth string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip git bookkeeping entirely: stepman copies the whole fixture dir
		// (including .git/) during `setup --local`, and git's hook samples,
		// object pack files and HEAD timestamps add noise without signal.
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		content, err := os.ReadFile(pth)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, pth)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(content)
		return nil
	})
	require.NoError(t, err)
	return files
}

func fileSummary(content string) string {
	if len(content) <= maxInlineContent {
		return content
	}
	return fmt.Sprintf("<%d bytes, sha256 %x>", len(content), sha256.Sum256([]byte(content)))
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
