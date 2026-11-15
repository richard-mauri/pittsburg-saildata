"""Regression tests for the generated suite policy gate (no network or repo writes)."""
import io
import pathlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from unittest.mock import patch

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import suite_policy as policy


class PolicyGateTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        self.docs = self.root / "docs"
        self.docs.mkdir()
        original = policy.COPY.read_bytes()
        (self.docs / "suite-development-policy.md").write_bytes(original)
        for name in policy.STANDARD:
            text = "Read docs/suite-development-policy.md" if name == "AGENTS.md" else "app-specific document"
            (self.root / name).write_text(text)
        self.original = original
        self.p1 = patch.object(policy, "ROOT", self.root)
        self.p2 = patch.object(policy, "COPY", self.docs / "suite-development-policy.md")
        self.p1.start()
        self.p2.start()
        self.addCleanup(self.p1.stop)
        self.addCleanup(self.p2.stop)

    def test_valid_offline_copy(self):
        fields, body = policy.check_local()
        self.assertEqual(policy.digest(body), fields["source_sha256"])

    def test_tampered_copy_rejected(self):
        path = self.docs / "suite-development-policy.md"
        path.write_bytes(self.original + b"tampered")
        with self.assertRaisesRegex(ValueError, "edited"):
            policy.check_local()

    def test_stale_upstream_rejected(self):
        fields, body = policy.check_local()
        fake_sha = "f" * 40
        with patch.object(policy, "download", side_effect=[
            ('{"sha":"' + fake_sha + '"}').encode(),
            body + b"new policy change"
        ]), patch.object(sys, "argv", ["suite_policy.py"]), redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as result:
                policy.main()
            self.assertEqual(result.exception.code, 1)

    def test_offline_does_not_claim_current_upstream(self):
        with patch.object(sys, "argv", ["suite_policy.py", "--offline"]), redirect_stdout(io.StringIO()) as output:
            policy.main()
            self.assertIn("UNVERIFIED", output.getvalue())


if __name__ == "__main__":
    unittest.main()
