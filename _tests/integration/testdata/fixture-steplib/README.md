# Stepman integration-test fixture steplib

A minimal steplib layout consumed by the golden parity suite in
`_tests/integration`. The runner copies this tree into a temp dir,
turns it into a git repo, and points `stepman` subcommands at it over
`file://`. Not used by any live step or by the public steplib index.

Contents:

- `steplib.yml` — minimal collection metadata.
- `steps/test-step/1.0.0/step.yml` — one well-formed step version.
