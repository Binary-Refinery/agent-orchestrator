# OpenCode Backbone resumption after workspace loss

Date: 2026-10-18. This record distinguishes recovered repository evidence from
new work. It is not a test result, independent review or activation approval.

## Recovered source

The work branch was cloned from its existing remote checkpoint
`dcd187616f8b92cd668e75184fd5d697ac40fc66`, tree
`166946128c6104a640edac5c301660067feb6ebc`. Its base remains
`6b1ba43f1df9415c1ec937876ce54c86aa37484e`. Both refs were checked on GitHub;
no AO PR existed for this branch at resumption. The checkout was initially clean.

The source tree is the exact previously preserved Full-review snapshot. The
fresh command `git diff --binary 6b1ba43f1df9415c1ec937876ce54c86aa37484e dcd187616f8b92cd668e75184fd5d697ac40fc66`
produces SHA-256 `8b8a9a50832853a083229c287ed477c2e10b8f426b65dcd26939889b0507e870`
in this checkout. New reviews bind this command and these immutable Git objects.
Historical patch digests are not substituted for this newly observed digest.

The approved plan, Task, independent plan review, backend report, auxiliary report
and independent quality audit were preserved in Git. Their historical executions
remain attributed to their original source snapshots and dates. The complete
backend race suite was recorded as failed, with all 23 failures reproduced on the
unchanged baseline. The later equivalent macOS path-expression correction had
its own full lint/build/vet and affected-package checks. Neither fact is rewritten
as a fresh complete-suite pass.

## Missing unpublished records

The prior local Full behavior/security reports, subsequent Windows workflow
repair, repair checks, Delta and closing triage were not present in the remote
checkpoint. A search for the named reports found no matching saved copy. Their
original bytes and the ignored raw local logs/manifests are unavailable here.
The retained conversation describes their outcomes, but those descriptions are
not presented as recovered report files or newly executed tests.

Two independent reviewers therefore perform fresh Full reviews of the recovered
immutable snapshot. A separate triager evaluates their findings before any repair.
Any confirmed repair receives new bounded checks, independent Delta review and
separate closure. The unchanged product does not receive a fictional rerun of its
historical complete suites. Actual GitHub CI after PR creation remains required.

## Boundaries carried forward

The existing authorization covers the paired PRs and visible Codex review. It
does not authorize merge, release, deployment, settings changes or downstream
activation. No blocked native adversarial reproduction is retried. Data-only
negative checks and supported benign validation remain available. Synthetic
qualification, canned-provider smoke, native platform observations and OS
confinement remain distinct claims.

Backbone PR 16 remains at
`4e2f1272e34c3472cb1afe559e87347b9e2579ec`. On 2026-10-18 its refreshed five CI
jobs were observed successful: 418 Foundation tests, trusted-base validation,
Docker/offline checks including 13 Copier tests, and native Linux/Windows smoke.
Codex's scoped advisory issue-comment receipt 6213996553 reported no major issues
on that exact head. This is separate producer evidence, not AO native Windows
readback, actual model qualification or OS confinement.
