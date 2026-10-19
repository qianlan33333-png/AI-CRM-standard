"""Real clean-checkout, source drift and machine handoff contracts."""
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import development_prepare as p
import manual_github_sync as sync

SOURCE = Path(__file__).resolve().parents[1]


class Preparation(unittest.TestCase):
    def clone(self, directory):
        root = Path(directory) / "source"
        subprocess.run(["git", "clone", "-q", "--shared", str(SOURCE), str(root)], check=True)
        subprocess.run(["git", "-C", str(root), "config", "user.name", "Preparation fixture"], check=True)
        subprocess.run(["git", "-C", str(root), "config", "user.email", "fixture@localhost"], check=True)
        return root

    def commit(self, root, path):
        p.git(root, "add", path)
        p.git(root, "-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixture")

    def test_committed_canonical_drift_is_bound_once_and_fresh_views_match(self):
        with tempfile.TemporaryDirectory() as d:
            root = self.clone(d); base = p.git(root, "rev-parse", "HEAD").decode().strip()
            api = root / "api/openapi.yaml"; api.write_bytes(api.read_bytes() + b"\n# fixture comment\n")
            self.commit(root, "api/openapi.yaml")
            changed = p.synchronize_sources(root, base, "authorized isolated test fixture")
            self.assertEqual(set(changed), {p.INDEX, p.LOCK, p.APPROVALS})
            subprocess.run(["node", "scripts/prepare-donor-source-views.mjs"], cwd=root, check=True, capture_output=True)
            self.assertEqual((root / "internal/config/http/openapi.yaml").read_bytes(), api.read_bytes())
            before = {path: (root / path).read_bytes() for path in changed}
            self.assertEqual(p.synchronize_sources(root, base, "authorized isolated test fixture"), [])
            self.assertEqual(before, {path: (root / path).read_bytes() for path in changed})
            p.git(root, "add", *changed); p.git(root, "-c", "core.hooksPath=/dev/null", "commit", "-qm", "prepared binding")
            import check_source_authority_changes as authority
            self.assertEqual(authority.check(root, base, "HEAD")["status"], "pass")

    def test_dirty_canonical_and_missing_authorization_do_not_rebind(self):
        with tempfile.TemporaryDirectory() as d:
            root = self.clone(d); base = p.git(root, "rev-parse", "HEAD").decode().strip()
            index = (root / p.INDEX).read_bytes()
            api = root / "api/openapi.yaml"; api.write_bytes(api.read_bytes() + b"\n# dirty\n")
            with self.assertRaisesRegex(ValueError, "not_committed"):
                p.synchronize_sources(root, base, "fixture")
            self.assertEqual((root / p.INDEX).read_bytes(), index)
            self.commit(root, "api/openapi.yaml")
            with self.assertRaisesRegex(ValueError, "scope_reference"):
                p.synchronize_sources(root, base, None)
            self.assertEqual((root / p.INDEX).read_bytes(), index)

    def test_noop_preserves_frozen_source_and_rejects_unsafe_or_unregistered_program(self):
        with tempfile.TemporaryDirectory() as d:
            root = self.clone(d)
            original = (root / p.INDEX).read_bytes()
            self.assertEqual(p.synchronize_sources(root, None, None), [])
            self.assertEqual((root / p.INDEX).read_bytes(), original)
            with self.assertRaises(ValueError):
                p.safe_file(root, "../outside")
            manifest = root / "docs/release-checks.json"
            value = json.loads(manifest.read_text()); value["build_commands"] = ["./cmd/not-a-release-program"]
            manifest.write_text(json.dumps(value))
            with self.assertRaisesRegex(ValueError, "unregistered_release_program"):
                p.prepare(root, generate_catalog=False)

    def test_handoff_rejects_old_failed_changed_and_unpushed_receipts(self):
        with tempfile.TemporaryDirectory() as d:
            root = self.clone(d); head = p.git(root, "rev-parse", "HEAD").decode().strip()
            receipt = Path(d) / "receipt.json"; impact = Path(d) / "impact.json"
            route = Path(d) / "workstation.json"; route.write_text('{"thread_id":"fixture-workstation"}')
            impact.write_text(json.dumps(dict.fromkeys(["contract", "mechanism", "consumers", "pages", "evidence"], "fixture")))
            raw_git = p.git
            raw_git(root, "remote", "add", "domestic", "aicrm-release-push@aicrm-v4-stage-source:/opt/aicrm/domestic/source.git")
            def fake_git(root, *args, env=None):
                if args[0] == "ls-remote":
                    self.assertIn("ProxyJump=none", env["GIT_SSH_COMMAND"])
                    self.assertIn("ProxyCommand=none", env["GIT_SSH_COMMAND"])
                    return (head + "\trefs/heads/codex/fixture\n").encode()
                return raw_git(root, *args, env=env)
            with patch.object(p, "git", side_effect=fake_git), patch.object(p, "WORKSTATION_ROUTE", route), \
                 patch.object(sync, "_git_ssh_command", return_value="ssh -o ProxyJump=none -o ProxyCommand=none"), \
                 patch.object(sync, "_resolve_ssh_target", return_value="115.190.160.247"):
                for row in ({"head": "a" * 40, "result": "passed"},
                            {"head": head, "result": "failed"},
                            {"head": head, "result": "passed", "source": {"unchanged_during_execution": False}}):
                    receipt.write_text(json.dumps(row))
                    with self.assertRaises(ValueError):
                        p.handoff(root, head, "refs/heads/codex/fixture", "fixture", "fixture", [receipt], impact, [])
                receipt.write_text(json.dumps({"head": head, "result": "passed"}))
                with self.assertRaisesRegex(ValueError, "no_executed_checks"):
                    p.handoff(root, head, "refs/heads/codex/fixture", "fixture", "fixture", [receipt], impact, [])
                log = Path(d) / "named.jsonl"
                log.write_text('{"Test":"TestReal","Action":"pass"}\n{"Action":"pass"}\n')
                step = {"command": ["go", "test", "-json", "-run", "^TestReal$", "./fixture"], "exit_code": 0, "log": str(log)}
                receipt.write_text(json.dumps({"head": head, "result": "passed", "steps": [step]}))
                row = p.handoff(root, head, "refs/heads/codex/fixture", "fixture", "fixture", [receipt], impact, ["Provider delivery"])
                self.assertEqual(row["head"], head)
                self.assertEqual(row["unverified"], ["Provider delivery"])
                log.write_text('PASS TestReal')
                with self.assertRaises(ValueError):
                    p.handoff(root, head, "refs/heads/codex/fixture", "fixture", "fixture", [receipt], impact, [])
            with patch.object(p, "git", side_effect=lambda root, *args, env=None: b"" if args[0] == "ls-remote" else raw_git(root, *args, env=env)), \
                 patch.object(p, "WORKSTATION_ROUTE", route), \
                 patch.object(sync, "_git_ssh_command", return_value="ssh -o ProxyJump=none -o ProxyCommand=none"), \
                 patch.object(sync, "_resolve_ssh_target", return_value="115.190.160.247"):
                with self.assertRaisesRegex(ValueError, "not_pushed"):
                    p.handoff(root, head, "refs/heads/codex/fixture", "fixture", "fixture", [receipt], impact, [])

    def test_handoff_rejects_old_or_proxy_ssh_route_before_remote_read(self):
        with tempfile.TemporaryDirectory() as d:
            root = self.clone(d)
            head = p.git(root, "rev-parse", "HEAD").decode().strip()
            raw_git = p.git
            raw_git(root, "remote", "add", "domestic", "aicrm-release-push@old-stage:/opt/aicrm/domestic/source.git")
            calls = []

            def track_git(root, *args, env=None):
                calls.append(args)
                if args[0] == "ls-remote":
                    return (head + "\trefs/heads/codex/fixture\n").encode()
                return raw_git(root, *args, env=env)

            with patch.object(p, "git", side_effect=track_git), \
                 patch.object(sync, "_resolve_ssh_target", side_effect=sync.SyncError("staging_ssh_host_not_allowlisted")):
                with self.assertRaisesRegex(ValueError, "ssh_route_not_approved"):
                    p.handoff(root, head, "refs/heads/codex/fixture", "fixture", "fixture", [], Path(d) / "missing-impact", [])
            self.assertFalse(any(args[0] == "ls-remote" for args in calls))


if __name__ == "__main__":
    unittest.main()
