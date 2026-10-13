# Fork CI on local runners

All 29 job definitions in this fork select self-hosted runners, including native
platform matrices, artifact aggregation and currently upstream-only jobs. Existing
fork/upstream publication conditions remain unchanged. Missing registrations leave
jobs queued; there is no automatic hosted fallback or removal of native checks.

## Required registrations

The `Binary-Refinery/agent-orchestrator` repository needs dedicated registrations
with these cumulative labels. `ao-ci` is a required deployment contract, **not** a
claim that a runner with this label already exists.

| Native tier | Required labels | Purpose |
| --- | --- | --- |
| Linux x64 | `self-hosted`, `linux`, `x64`, `ao-ci`, `ci-heavy` | Go, frontend, containers, checks and aggregates |
| Windows x64 | `self-hosted`, `windows`, `x64`, `ao-ci` | Windows installer, workspace, CLI and upgrade checks |
| macOS ARM64 | `self-hosted`, `macOS`, `ARM64`, `ao-ci` | Existing latest/macOS14 native tiers and ARM artifact |
| macOS Intel | `self-hosted`, `macOS`, `x64`, `ao-ci` | Separate Intel desktop artifact |

The organization runners used by `Artifaktory/Artifaktory` do not prove access for
this user-owned fork. Existing Linux build jobs have run on `pc1-build`/`pc2-build`
in `af-ci-build-v1`; none of those observations establishes the registrations
above. Windows/macOS registrations and fork access remain unverified.

## Host prerequisites and activation

- Use isolated CI machines/users and clean job workspaces, separate from developer
  AO data, credentials and unrelated repositories. A repository label alone does
  not establish isolation or access control. Verify repository-wide runner access
  and approval restrictions before activation.
- Linux needs Docker for the CLI container, pinned Playwright image and secret
  scanner. Linux desktop packages run inside Ubuntu 22.04 containers to retain the
  supported glibc baseline; those containers install native build and digest tools.
- Native hosts need Bash/Git and the toolchains selected by the existing setup
  actions. macOS also needs Xcode command-line tools, Swift and `lipo`; Windows
  needs Git Bash. Keep the OS/architecture tiers above intact.
- Candidate validation uses read-only tokens. Existing `pull_request` events and
  default merge-result checkout are preserved. Same-repository job guards mitigate
  ordinary external-fork dispatch but are editable by a candidate; they are not an
  immutable security boundary. No new `pull_request_target` execution is added.
- Keep the PR draft until registrations, isolation, repository access and actual
  CI job records identify the intended own runners. This change does not register
  runners, change GitHub settings, merge, publish or deploy.

## Verification

`python -m unittest discover -s test/ci -v` uses PyYAML and a checkout containing
the pinned trusted base `f60f432a32b9f72370dafab2c58b703182dc8026`. It checks every
job selection, native matrix coverage, retained commands/events/timeouts, PR merge
checkout, artifact identities, Ubuntu22.04 bootstrap and rejection of hosted
fallback/missing labels/missing tiers. These are routing/contract checks, not proof
of a real GitHub runner executing a job. Actionlint may use a local custom-label
configuration containing `ao-ci` and `ci-heavy`.

Source validation failures and unavailable native/container execution are recorded
explicitly in the review evidence. Previous passing hosted CI is source evidence,
not local-runner activation evidence. Revert through a reviewed PR if activation
fails; do not silently route jobs back to hosted machines.

GitHub references: [runner labels and groups](https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/use-in-a-workflow),
[untrusted checkout boundaries](https://docs.github.com/en/actions/reference/security/secure-use).
