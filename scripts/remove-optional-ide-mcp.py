#!/usr/bin/env python3
"""Remove one explicitly named optional IDE MCP server; never start the IDE."""

from __future__ import annotations

import argparse
import copy
import json
import os
from pathlib import Path
import re
import stat
import tempfile
import tomllib


def table_path(header: str) -> list[str] | None:
    try:
        node = tomllib.loads(header)
    except tomllib.TOMLDecodeError:
        return None
    keys: list[str] = []
    while isinstance(node, dict) and len(node) == 1:
        key, node = next(iter(node.items()))
        keys.append(key)
    return keys if node == {} else None


def json_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    value: dict[str, object] = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate JSON keys cannot be edited safely")
        value[key] = item
    return value


def remove_server(text: str, config_format: str, server_name: str) -> tuple[str, str | None]:
    parse = tomllib.loads if config_format == "codex" else lambda value: json.loads(value, object_pairs_hook=json_object)
    before = parse(text)
    if not isinstance(before, dict):
        raise ValueError("configuration must be an object")
    root_key = "mcp_servers" if config_format == "codex" else "mcp"
    servers = before.get(root_key, {})
    if not isinstance(servers, dict):
        raise ValueError(f"{root_key} must be an object")
    matches = [key for key in servers if key.casefold() == server_name.casefold()]
    if not matches:
        return text, None
    if len(matches) != 1:
        raise ValueError("server name is ambiguous; use an unambiguous configuration")
    key = matches[0]
    expected = copy.deepcopy(before)
    del expected[root_key][key]
    if config_format == "opencode":
        return json.dumps(expected, ensure_ascii=False, indent=2) + "\n", key

    # Retain unrelated TOML bytes and comments. Parsed equality below also
    # guards apparent table headers inside multiline strings and inline tables.
    lines = text.splitlines(keepends=True)
    result: list[str] = []
    dropping = False
    for line in lines:
        if re.match(r"^\s*\[.+\]\s*(?:#.*)?$", line.rstrip("\r\n")):
            path = table_path(line)
            if path is not None:
                dropping = path[:2] == [root_key, key]
        if not dropping:
            result.append(line)
    updated = "".join(result)
    after = parse(updated)
    # An implicit empty parent disappears when its only child is removed.
    if not expected[root_key] and root_key not in after:
        del expected[root_key]
    if after != expected:
        raise ValueError("configuration layout cannot be edited safely; no file changed")
    return updated, key


def repair(path: Path, config_format: str, server_name: str, apply: bool) -> tuple[str | None, Path | None]:
    if path.is_symlink() or not path.is_file():
        raise ValueError("configuration must be an explicit regular file")
    original = path.read_bytes()
    # utf-8-sig accepts a Windows BOM and preserves it on writeback.
    bom = original.startswith(b"\xef\xbb\xbf")
    text = original.decode("utf-8-sig")
    updated, removed = remove_server(text, config_format, server_name)
    if removed is None or not apply:
        return removed, None
    mode = stat.S_IMODE(path.stat().st_mode)
    encoded = updated.encode("utf-8")
    if bom:
        encoded = b"\xef\xbb\xbf" + encoded
    backup: Path | None = None
    staging: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(prefix=path.name + ".ao-backup-", dir=path.parent, delete=False) as output:
            backup = Path(output.name)
            output.write(original)
        os.chmod(backup, mode)
        with tempfile.NamedTemporaryFile(prefix=path.name + ".ao-edit-", dir=path.parent, delete=False) as output:
            staging = Path(output.name)
            output.write(encoded)
        os.chmod(staging, mode)
        if path.read_bytes() != original:
            raise ValueError("configuration changed during editing; no replacement performed")
        os.replace(staging, path)
        staging = None
        return removed, backup
    finally:
        if staging is not None:
            staging.unlink(missing_ok=True)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--format", choices=("codex", "opencode"), required=True)
    parser.add_argument("--server", required=True, help="Exact configured server key, e.g. pycharm")
    parser.add_argument("--apply", action="store_true", help="Apply with a byte-for-byte backup; otherwise preview")
    args = parser.parse_args()
    if not args.server.strip():
        parser.error("server name cannot be empty")
    try:
        removed, backup = repair(args.config, args.format, args.server, args.apply)
    except (OSError, UnicodeError, ValueError) as error:
        parser.exit(1, f"No configuration replacement completed: {error}\n")
    if removed is None:
        print("Named server not present; no changes.")
    elif backup is None:
        print(f"Preview: remove optional MCP server {removed!r}; use --apply to write a backup and apply.")
    else:
        print(f"Removed optional MCP server {removed!r}. Backup: {backup}")


if __name__ == "__main__":
    main()
