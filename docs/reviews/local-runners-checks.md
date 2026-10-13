# Local runner validation evidence

Base: `f60f432a32b9f72370dafab2c58b703182dc8026`.

- `python -m unittest discover -s test/ci -v`: PASS, six tests.
- Actionlint 1.7.7 with explicit local `ao-ci`/`ci-heavy` labels and shellcheck disabled: PASS. This validates YAML/expressions, not runner registrations or Bash lint.
- `git diff --check`: PASS.
- The production source is unchanged from the base; a separate quality checker is auditing recorded exact-source checks and missing checks. Its final audit follows before PR delivery. Source failures are not treated as routing successes or silently fixed in this scoped change.
- No Docker, native Windows/macOS or matching Node20 runtime in the local environment. Native/container/package execution and runner access remain unverified. No release/deployment validation is executed.
- Draft activation requires actual own-runner job records, dedicated registrations, isolation and access evidence.
