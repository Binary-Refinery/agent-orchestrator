# Instructor upstream integration boundary

Baseline inspected on 2026-10-12:

- Fork main: `f24bfcf265d5f9df657e71145a842423b3cb7f19` (PRs 14 and 15 merged).
- Upstream: `OrchestratorInc/agent-orchestrator`,
  `c3e17362a85f37d0b0154bf8d841ed4f633f8306`.
- Previous upstream audit: `95dd4d712d5b6026cc00d59fd0dd63aea5c4e66e`.
- Direct tree comparison: 2,344 changed files, +252,955/−63,160 lines. This is
  source drift, not a count of commits or an already-resolved merge.
- Shared migration files through version 164 are byte-identical at these refs.
  Upstream's latest migration is 194. New feature migrations reserve numbers
  above that observed maximum; this does not make the two lineages compatible.

## Known conflicts

| Version | Fork meaning | Upstream meaning |
| --- | --- | --- |
| 165 | Instructor works, associations, immutable audit/snapshots and dispatches | MiMo Code harness |
| 166 | Native reviewer launch configuration | DeepSeek harness |
| 167 | Governed semantic delivery ledger | OpenCode v2 harness |
| 168 | Work/phase-scoped independent review runs | Cues |
| 169 | Durable Instructor planning jobs | Reported-PR CDC |
| 170 | Instructor inventory cache/CDC | Native review-result notifications |

`adapters/agent/modelcatalog/opencode_default.go` also diverges: the fork resolves
the native effective default through OpenCode configuration readback and pins
managed settings; upstream has a conservative layered resolver. Preserve the
fork's exact native model/variant validation, requested/passed/confirmed provenance,
review independence, work-scoped gates and publication guards when reconciling
those implementations. Taking either file wholesale does not prove equivalence.

## Separate merge acceptance

Prepare a normal isolated merge into fork main after retrieving the common Git
history. Preserve the published fork history and all already-merged SQL files.
Do not rename or rewrite migrations 165–170 to make Goose accept duplicate
numbers. An additive, explicit lineage bridge must recognize physical schemas
and retain both sets of applied effects and user data; a version-only rewrite
can silently skip needed schema and is not acceptable.

The merge must exercise independent fresh, populated pre-merge fork and populated
upstream database paths, including intermediate collision versions. Verify real
work/session/review history, immutable snapshots and events, FK/CDC consistency,
native launch configuration, publication grants, dispatch/restart fencing,
automation and legacy feature-off behavior. Run the complete workflow suites and
independent delta review against the final merged head.

The feature PR's lineage guard intentionally rejects incompatible upstream
databases before schema/history repairs. It must remain until the merge's bridge
and complete upgrade matrix pass. Its actual upstream fixtures live under
`backend/internal/storage/sqlite/testdata/upstream-c3e17362/`; the manifest records
their source ref and checksums. They are test data, not production migrations.

Installer `0.13.0-artifaktory.2`, unspecified MCP fixes and other local process
issues require a concrete build/runtime inventory before being claimed complete.
The existing unsigned Windows build workflow accepts a pinned ref/version and
requires `VITE_WORKOS_CLIENT_ID`; it does not publish a release. This plan neither
starts a release nor changes the updater feed. The requested PyCharm MCP removal
and repository ownership handling belong to their explicit implementation slice.

## Policy settings follow-up boundary

The post-PR-16 [policy settings repair](instructor-policy-settings-validation.md)
is a frontend compatibility fix on fork main. Existing policies can contain
`command: null`; displaying that value safely requires no upstream merge,
policy-hash normalization, database rewrite or migration change. The integration
and installed-build validation requirements above remain separate work.
