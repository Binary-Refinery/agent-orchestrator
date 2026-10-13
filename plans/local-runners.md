# Local CI runner migration

Human request: 2026-10-13, continue Instructor orchestration planning and move the two repositories’ workflow jobs onto local runners through reviewed PRs. No merge, release, registration or repository-setting write is authorized here.

## Scope and acceptance

- Use clean, isolated checkouts from current main: AO f60f432a32b9f72370dafab2c58b703182dc8026; Backbone 2cd4d9bd947fbd1aff25c6bec28e98f3fc1b1c13.
- Preserve every test command, native OS/architecture tier, timeout, artifact identity and aggregate gate. No automatic hosted fallback when local runners are missing.
- Backbone source jobs use verified organization group af-ci-build-v1 with labels self-hosted/linux/x64/ci-heavy. Reusable workflows keep ubuntu-latest for other consumer repositories; routing depends on repository identity, never caller-controlled source_mode. Test source/consumer routing and rejected failure/skipped mandatory jobs.
- AO uses dedicated repository registrations with ao-ci plus exact OS/architecture labels. Existing organization runners are not presumed accessible to a public user-owned repo. Native Windows/macOS registrations are missing/unverified. Preserve matrix display names explicitly. The macos-latest/macOS14 native tiers retain ARM64; the separate Intel artifact tier retains x64.
- AO preserves the existing pull_request triggers and their default merged-result checkout. Same-repository job guards mitigate ordinary foreign-fork dispatch but are candidate-editable, not immutable protection. Use read-only tokens for candidate validation; preserve existing explicitly trusted/default refs. Repository-wide runner-access/approval controls and dedicated clean runner isolation must be independently verified before activation in BOTH repositories. Do not introduce privileged pull_request_target candidate execution as part of a runner migration.
- Preserve Ubuntu22.04/glibc baseline for both Linux desktop build paths with job containers and required bootstrap tools. Docker access is an activation prerequisite.
- Backbone event/trust contracts remain unchanged; candidate-editable fork guards are only mitigation, not immutable protection.

## Verification and delivery

Run producer/consumer and negative routing checks, unchanged affected suites, Backbone source/trusted validators, compiled-toolchain/workflow-kit checks and diff whitespace checks. Run all feasible AO CI validation jobs with pinned runtimes; record unavailable Docker, native OS, network/dependency checks as blocked, never PASS. Independent read-only plan review precedes implementation; Full reviewers share a frozen base/head/patch digest. Triage precedes repair and scoped Delta reviews. Maximum two Full rounds and six repair iterations. Publish scoped draft PRs if registration, isolation or remote runtime evidence remains unavailable. Trigger requested advisory Codex review with visible PR evidence; no automated merge. PR candidate CI must not be presented as local-runner runtime proof until the returned job records name the intended runners and groups. Consumer routing, failed/skipped mandatory gates, default PR merge checkout and all original platform tiers need regression coverage.

## Current runner evidence

AF build run 37198308243 jobs 111424627445/111424627576: pc1-build/pc2-build, group af-ci-build-v1, labels self-hosted/linux/x64/ci-heavy. Today’s run 38379065325 confirms local operation but legacy ci-heavy also reaches fast runners, requiring group binding for Backbone. Backbone run 38371182744 and 42 sampled AO jobs use hosted runners. Group repository access for engineering-backbone is not yet observed.

## Recovery

Until merge the default-branch workflows remain unchanged. If activation fails, leave the PR draft and supply missing registration/group-access evidence. A human can revert this scoped workflow change through a separate PR. Do not silently re-enable hosted runners or drop a native check.
