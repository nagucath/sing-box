from __future__ import annotations

import importlib.util
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]


class H3CModulePatchTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        root = Path(self.temporary.name)
        package = root / "patches/sing-openconnect"
        shutil.copytree(ROOT / "patches/sing-openconnect", package)
        spec = importlib.util.spec_from_file_location("fixture_h3c_patches", package / "run.py")
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        for item in self.module.inventory():
            target = str(item["target"])
            if target.endswith(".go") and not target.endswith("_test.go"):
                relative = Path("vendor/github.com/sagernet/sing-openconnect") / target
                destination = root / relative
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / relative, destination)
        self.root = root

    def test_offline_vendor_verification_never_runs_a_command(self) -> None:
        with patch.object(self.module, "run", side_effect=AssertionError("unexpected acquisition")):
            self.module.verify_vendor()
            self.assertEqual(len(self.module.metadata()["patches"]), 5)

    def test_rejects_changed_projected_production_source(self) -> None:
        source = self.root / "vendor/github.com/sagernet/sing-openconnect/h3c_auth.go"
        source.write_bytes(source.read_bytes() + b"\n// changed\n")
        with self.assertRaisesRegex(RuntimeError, "Go vendor projection differs"):
            self.module.verify_vendor()

    def test_rejects_changed_patch_bytes(self) -> None:
        item = self.module.inventory()[0]
        filename = self.module.PACKAGE / str(item["path"])
        filename.write_bytes(filename.read_bytes() + b"\n")
        with self.assertRaisesRegex(RuntimeError, "patch digest mismatch"):
            self.module.metadata()


if __name__ == "__main__":
    unittest.main()
