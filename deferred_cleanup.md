Here's what I found outside your scope that needs attention:

activator/steplib_ref.go:97

fmt.Errorf("setup %s: %s", id.SteplibSource, err)  // → %w
activator/steplib/activate.go (4 lines)

:30  "failed to read %s steplib: %s", stepLibURI, err    → %w
:35  "failed to find step: %s", err                      → %w
:85  "failed to check if %s path exist: %s", dest, err   → %w
:93  "copy command failed: %s", err                      → %w
activator/steplib/activate_executable.go (3 lines)

:66  "validate hash: %s", err                          → %w  (validateHash wraps io.Copy errors)
:71  "set executable permission on file: %s", err      → %w
:75  "copy step.yml: %s", err                          → %w
(Lines 33, 44, 50, 61 in the same file already use %w — these three are stragglers.)

activator/steplib/activate_source.go (6 lines)

:30  "failed to check if %s path exist: %s", stepCacheDir, err   → %w
:41  "download failed: %s", err                                   → %w
:46  "copy step failed: %s", err                                  → %w
:50  "copy step.yml failed: %s", err                              → %w
:58  "failed to check if %s path exist: %s", dst, err             → %w
:61  "failed to create dir for %s path: %s", dst, err             → %w
:66  "copy command failed: %s", err                               → %w
(:36 is fine — errMsg there is a string, not an error.)

models/version_constraint.go (6 lines)

:27, :31, :35   Semver parsing — strconv.Atoi errors formatted with %s  → %w
:108, :126, :143  VersionConstraint parsing — same pattern               → %w
Lower-priority (old V1 code): stepman/library.go, stepman/step_info.go, stepman/util.go, cli/, toolkits/, preload/ all have scattered %s-for-err — happy to list those too if you want to sweep them at once.