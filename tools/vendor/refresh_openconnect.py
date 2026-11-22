#!/usr/bin/env python3
"""Regenerate the OpenConnect adapter package in its isolated Actions workspace."""
from __future__ import annotations
import difflib
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
PACKAGE = ROOT / "patches/sing-openconnect"


def main() -> None:
    spec = importlib.util.spec_from_file_location("openconnect_patches", PACKAGE / "run.py")
    package = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(package)
    source = package.prepare()
    info = json.loads(package.run("go", "mod", "download", "-json", source["module"] + "@" + source["version"]))
    pristine = Path(info["Dir"])
    work = package.OUTPUT
    targets = package.inventory()
    subprocess.run(["gofmt", "-w", *[str(work / str(item["target"])) for item in targets if str(item["target"]).endswith(".go")]], check=True)
    subprocess.run(["go", "mod", "tidy"], cwd=work, check=True)
    records = []
    for item in targets:
        target = str(item["target"])
        original = pristine / target
        before = original.read_bytes() if original.exists() else b""
        after = (work / target).read_bytes()
        def blob(data: bytes) -> str:
            return hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        data = f"diff --git a/{target} b/{target}\n"
        if not original.exists():
            data += "new file mode 100644\n"
        data += f"index {blob(before) if original.exists() else '0' * 40}..{blob(after)}" + (" 100644" if original.exists() else "") + "\n"
        data += "".join(difflib.unified_diff(before.decode().splitlines(True), after.decode().splitlines(True), fromfile="a/" + target if original.exists() else "/dev/null", tofile="b/" + target))
        path = PACKAGE / str(item["path"])
        path.write_text(data)
        records.append(f"{item['order']} {item['path']} {hashlib.sha256(path.read_bytes()).hexdigest()}")
    (PACKAGE / "apply_order.txt").write_text("\n".join(records) + "\n")
    metadata = json.loads((PACKAGE / "source.json").read_text())
    metadata["result_sha256"] = package.tree_digest(work)
    (PACKAGE / "source.json").write_text(json.dumps(metadata, indent=2) + "\n")


if __name__ == "__main__":
    main()
