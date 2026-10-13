# Local runner validation evidence

Base: `f60f432a32b9f72370dafab2c58b703182dc8026`.

- `python -m unittest discover -s test/ci -v`: PASS, seven tests after the two confirmed runtime prerequisite repairs.
- Actionlint 1.7.7 with explicit local `ao-ci`/`ci-heavy` labels and shellcheck disabled: PASS. This validates YAML/expressions, not runner registrations or Bash lint.
- `git diff --check`: PASS.
- The existing production source is unchanged from the base. The independent final audit is recorded in `local-runners-quality.md`/`.json`. Build/vet, schema generation, frontend typechecks, CLI E2E and pinned lint passed; full frontend tests failed (5,567 pass, seven fail, six skip), and the broad backend package run timed out with actual failures. These are not silently repaired or certified green in this runner-only change.
- No Docker, native Windows/macOS or matching Node20 runtime in the local environment. Native/container/package execution and runner access remain unverified. No release/deployment validation is executed.
- Actual renderer mount preflight: four explicit UID/permission-boundary cases PASS (supported UID, rejected UID, tool-cache denial, command-file denial); this is not a Docker/native-host execution claim.
- Pinned Node24 leaderboard dependency repair: existing five Node contract tests PASS.
- Draft activation requires actual own-runner job records, dedicated registrations, isolation, per-tier native tool/UID provisioning and access evidence.
