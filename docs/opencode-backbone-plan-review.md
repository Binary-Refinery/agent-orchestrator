# Independent OpenCode Backbone reconstruction plan review

Date: 2026-10-16. Reviewer: independent `reconstruction_plan_review` session.
Role: read-only plan reviewer, separate from implementation authors.

## Inspected snapshot

- AO base and current committed HEAD:
  `6b1ba43f1df9415c1ec937876ce54c86aa37484e`.
- `docs/opencode-backbone-plan.md`, SHA-256
  `f548cd3566247a710ab3680b7213f2236cb15e88d7e81d624683957ac1d9dce4`.
- `docs/opencode-backbone-task.json`, SHA-256
  `85c10817af006fa665afa5e543746c510cabf3869b1627712c2f16a11062ebe7`.
- Linked Backbone plan at base `bf8e901ecd066e8e6d1b05f0c56a66081a473c0a`,
  plan SHA-256 `76a9695817447bfa6517499b1928cc8baa526135e5bd163c23dc439f5aa63f34`.

AO plan and packet hashes were rechecked unchanged after inspection. These
content hashes bind the preimplementation plan review; no implementation Full
review or implementation test result is claimed. This session writes review
records only and performs no remote publication.

## Conclusion

**NO CONFIRMED IN-SCOPE FINDINGS. The inspected AO reconstruction plan may proceed
to implementation within its recorded scope.**

The plan fits current domain policy hashing, Instructor admission, detached ACP
host and session-interface boundaries. It preserves absent binding and ungoverned
behavior, introduces no second state machine, and requires validation before
native probes/start plus revalidation at actual queued dispatch. Fresh controlled
processes, blocked live adoption and immutable model/config/interface binding
address the retained-host and native setter boundaries explicitly.

The generated `ao-default` prompt and permission projection are addressed directly;
installed Backbone agent files alone are not considered proof of selection. The
plan retains explicit stronger denies, admits only scoped implementer asks,
rejects bypass, and separates static native input checks from bounded native
readback. Selected skill receipt hashes and fixed raw Git blob checks for other
canonical skills close the declared discovery input set without executing candidate
Python or fetching mutable references. A real producer-generated receipt fixture
is a required interoperability check.

Qualification is explicitly operator attestation with no seeded success or claim
of signature authentication. Synthetic admission, native canned-provider behavior,
real-model qualification and OS isolation remain distinct. Unsupported TUI and
read-only OpenCode confinement remain blocked rather than inferred from native
permission syntax. The current runtime's locked workflow and fix-round policy
remain unchanged; the task's development repair budget does not revise them.

The verification/publication sequence follows AO's complete local CI requirement,
records unavailable platform jobs, verifies remote results, and maintains required
PR category counts. Pending workbranch preservation is compatible with these
rules and does not constitute PR acceptance, release or activation. Existing user
authorization covers the scoped work; this review adds no merge or deployment
authority.

## Coverage and limits

Inspected the exact plan and packet, `AGENTS.md`, the PR-description skill, relevant
architecture description, domain policy types, policy hashing/parsing/locked
constraints, OpenCode ACP agent configuration generation, and relevant dispatch,
controller, session and adapter entry points. Checked the Go workflow's full race,
lint, generation and Windows obligations. Compared the contract with the linked
Backbone plan, current Task Packet grammar and navigation receipt producer,
including explicit Task Packet inclusion in receipt documents.

Data-only validation of this packet against the linked trusted-base Backbone Task
Packet schema passed (exit 0). No implementation, end-to-end lifecycle, native
runtime, remote CI, qualification or model test was performed. Exact new source
behavior, platform path handling and generated API consistency remain required
implementation checks and independent Full-review coverage. No previously blocked
dynamic reproduction was retried.
