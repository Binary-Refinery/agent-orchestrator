import importlib.util
import json
from pathlib import Path
import tempfile
import tomllib
import unittest


SPEC = importlib.util.spec_from_file_location("ide_repair", Path(__file__).with_name("remove-optional-ide-mcp.py"))
repair = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(repair)


class OptionalIdeRepairTests(unittest.TestCase):
    def test_codex_preserves_other_servers_settings_and_comments(self):
        text = '# operator settings\nmodel = "native-model"\n[mcp_servers."PyCharm"]\nurl = "http://localhost:64342/sse"\n[mcp_servers."PyCharm".env]\nIDE_HINT = "local"\n# keep this other server\n[mcp_servers.ao]\ncommand = "ao"\n'
        updated, name = repair.remove_server(text, "codex", "pycharm")
        self.assertEqual(name, "PyCharm")
        self.assertEqual(tomllib.loads(updated), {"model": "native-model", "mcp_servers": {"ao": {"command": "ao"}}})
        self.assertIn("# operator settings", updated)
        self.assertIn('[mcp_servers.ao]\ncommand = "ao"\n', updated)

    def test_missing_server_is_idempotent_and_never_removes_other_ide(self):
        text = '[mcp_servers.jetbrains]\ncommand = "unrelated"\n'
        self.assertEqual(repair.remove_server(text, "codex", "pycharm"), (text, None))

    def test_ambiguous_server_and_duplicate_json_keys_are_rejected(self):
        with self.assertRaisesRegex(ValueError, "ambiguous"):
            repair.remove_server('[mcp_servers.PyCharm]\ncommand="one"\n[mcp_servers.PYCHARM]\ncommand="two"\n', "codex", "pycharm")
        with self.assertRaisesRegex(ValueError, "duplicate JSON"):
            repair.remove_server('{"mcp":{"pycharm":{}},"model":"first","model":"second"}', "opencode", "pycharm")

    def test_inline_table_refuses_instead_of_rewriting_other_settings(self):
        with self.assertRaisesRegex(ValueError, "cannot be edited safely"):
            repair.remove_server('mcp_servers = { pycharm = { url = "http://localhost" }, ao = { command = "ao" } }\n', "codex", "pycharm")

    def test_table_looking_multiline_text_does_not_remove_unrelated_settings(self):
        text = 'notes = """\n[mcp_servers.pycharm]\nkeep this text\n"""\n[mcp_servers.pycharm]\nurl = "http://localhost"\n'
        with self.assertRaises(ValueError):
            repair.remove_server(text, "codex", "pycharm")

    def test_apply_requires_opt_in_and_retains_exact_backup_and_bom(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.toml"
            original = b'\xef\xbb\xbfmodel = "native"\r\n[mcp_servers.pycharm]\r\nurl = "http://localhost"\r\n'
            path.write_bytes(original)
            self.assertEqual(repair.repair(path, "codex", "pycharm", False), ("pycharm", None))
            self.assertEqual(path.read_bytes(), original)
            name, backup = repair.repair(path, "codex", "pycharm", True)
            self.assertEqual(name, "pycharm")
            self.assertEqual(backup.read_bytes(), original)
            self.assertEqual(path.read_bytes(), b'\xef\xbb\xbfmodel = "native"\r\n')
            self.assertEqual(repair.repair(path, "codex", "pycharm", True), (None, None))

    def test_opencode_removes_only_named_mcp_and_preserves_effort(self):
        text = json.dumps({"mcp": {"pycharm": {"url": "http://localhost"}, "ao": {"command": ["ao"]}}, "model": "provider/model", "variant": "ultra"})
        updated, _ = repair.remove_server(text, "opencode", "pycharm")
        self.assertEqual(json.loads(updated), {"mcp": {"ao": {"command": ["ao"]}}, "model": "provider/model", "variant": "ultra"})


if __name__ == "__main__":
    unittest.main()
