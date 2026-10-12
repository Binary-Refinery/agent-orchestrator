# Optional IDE and Git ownership recovery

An optional IDE MCP endpoint is not a prerequisite for terminal/API development,
checks or independent review. If PyCharm is not running, record its optional
probe as unavailable and continue the authorized work. Never claim that a
missing mandatory check passed, and do not launch an IDE just to unblock an
optional integration.

## Remove an unused PyCharm MCP entry

AO does not install or version a PyCharm MCP server in this fork. The normal
"Open in PyCharm" editor action is separate and remains available. If an agent's
local config contains an unused PyCharm MCP entry, remove that exact entry with
the portable helper. It requires Python 3.11 or newer and an explicit path and
server key. It never probes the network or starts PyCharm.

For a Codex TOML configuration, first preview the change in PowerShell:

```powershell
python scripts/remove-optional-ide-mcp.py --config "$env:USERPROFILE\.codex\config.toml" --format codex --server pycharm
```

After reviewing the preview, the same command with `--apply` removes the named
server and its nested settings, preserving unrelated TOML settings and an exact
backup. Use the actual configured server key if it is named differently; the
helper does not infer servers from an endpoint/port or remove other JetBrains
entries.

```powershell
python scripts/remove-optional-ide-mcp.py --config "$env:USERPROFILE\.codex\config.toml" --format codex --server pycharm --apply
```

For an explicitly identified OpenCode JSON file, use `--format opencode` and
its actual path. This route preserves unrelated JSON values and formats the
remaining document. JSONC, ambiguous names, inline TOML layouts that cannot be
edited safely, and symlinks fail without replacing the original. The printed
backup path contains the unchanged original, including a Windows UTF-8 BOM.
Restart the affected agent after applying its native configuration change.

This helper changes only the explicitly supplied local file when run. A source
PR does not edit an operator's already running machine or prove an IDE handshake.

## Bounded Git reads

Instructor inventory, source evidence and check HEAD reads share a bounded
read helper. After Git's actual `detected dubious ownership` refusal (exit 128),
it retries exactly once with `-c safe.directory=<canonical registered root>`.
Both attempts share the read deadline. Other failures and cancellation remain
failures; raw diagnostics are not exposed as evidence.

The exception lasts only for that invocation. No global config entry or wildcard
trust is added. Registered repository roots are the authority; Git's suggested
stderr path is not used to grant trust to another repository. Native worker
shells should use the same precise per-command workaround after an ownership
refusal instead of abandoning a valid task or setting `safe.directory=*`.

The regression test uses Git's own ownership check and verifies both a successful
registered HEAD read and that an ordinary subsequent Git invocation is still
refused. Configuration-removal tests cover unrelated settings, absent servers,
ambiguous/unsupported layouts, backups, BOMs and repeat application.
