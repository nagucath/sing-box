#!/usr/bin/env python3
"""Verify and apply sing-openconnect's package-owned Go module patches."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tempfile

PACKAGE = Path(__file__).resolve().parent
ROOT = PACKAGE.parents[1]
OUTPUT = ROOT / ".vendor-work/sing-openconnect"


def run(*args: str, cwd: Path = ROOT) -> str:
    environment = os.environ.copy()
    if args[0] == "git":
        environment["GIT_CEILING_DIRECTORIES"] = str(cwd.parent)
    result = subprocess.run(args, cwd=cwd, text=True, capture_output=True, check=False, env=environment)
    if result.returncode:
        raise RuntimeError((result.stderr or result.stdout).strip()[:6000])
    return result.stdout


def tree_digest(root: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise RuntimeError(f"symlink in module input: {path.name}")
        if path.is_file():
            name = path.relative_to(root).as_posix().encode()
            digest.update(len(name).to_bytes(4, "big"))
            digest.update(name)
            digest.update(hashlib.sha256(path.read_bytes()).digest())
    return digest.hexdigest()


def inventory() -> list[dict[str, str | int]]:
    entries = []
    targets = set()
    for line in (PACKAGE / "apply_order.txt").read_text().splitlines():
        if not line.strip() or line.startswith("#"):
            continue
        number, relative, expected = line.split()
        name = PurePosixPath(relative)
        if int(number) != len(entries) + 1 or name.is_absolute() or ".." in name.parts:
            raise RuntimeError("invalid patch order or path")
        if not re.fullmatch(r"[0-9a-f]{64}", expected):
            raise RuntimeError("invalid patch digest")
        path = PACKAGE / relative
        data = path.read_bytes()
        if hashlib.sha256(data).hexdigest() != expected:
            raise RuntimeError(f"patch digest mismatch: {relative}")
        changes = re.findall(rb"^diff --git a/(\S+) b/(\S+)$", data, re.M)
        if len(changes) != 1 or changes[0][0] != changes[0][1]:
            raise RuntimeError(f"patch must own one exact target: {relative}")
        target = changes[0][0].decode()
        target_path = PurePosixPath(target)
        if target_path.is_absolute() or ".." in target_path.parts:
            raise RuntimeError("invalid patch target")
        if not relative.endswith("/" + target + ".patch") or target in targets:
            raise RuntimeError("duplicate target or incorrect semantic patch path")
        targets.add(target)
        entries.append({"order": int(number), "path": relative, "sha256": expected, "target": target})
    if not entries or {item["path"] for item in entries} != {
        path.relative_to(PACKAGE).as_posix() for path in PACKAGE.rglob("*.patch")
    }:
        raise RuntimeError("patch inventory is empty or incomplete")
    return entries


def metadata() -> dict:
    source = json.loads((PACKAGE / "source.json").read_text())
    patches = inventory()
    return {**source, "path": ".vendor-work/sing-openconnect", "patches": patches}


def prepare(*, check_only: bool = False, check_vendor: bool = False) -> dict:
    source = metadata()
    patches = source["patches"]
    info = json.loads(run("go", "mod", "download", "-json", source["module"] + "@" + source["version"]))
    if info.get("Sum") != source["sum"] or "Error" in info:
        raise RuntimeError("Go module checksum does not match the source pin")
    pristine = Path(info["Dir"])
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="sing-openconnect-", dir=OUTPUT.parent) as temporary:
        staged = Path(temporary) / "module"
        shutil.copytree(pristine, staged)
        staged.chmod(0o755)
        for path in staged.rglob("*"):
            path.chmod(0o755 if path.is_dir() else 0o644)
        for patch in patches:
            filename = str(PACKAGE / str(patch["path"]))
            run("git", "apply", "--no-index", "--check", filename, cwd=staged)
            run("git", "apply", "--no-index", filename, cwd=staged)
        expected = source["result_sha256"]
        if tree_digest(staged) != expected:
            raise RuntimeError("patched module differs from the declared result")
        if check_vendor:
            verify_vendor(staged)
        if OUTPUT.exists():
            if tree_digest(OUTPUT) != expected:
                raise RuntimeError("materialized module contains edits; preserve them before rebuilding")
        elif not check_only:
            os.replace(staged, OUTPUT)
    return source


def verify_vendor(module: Path | None = None) -> None:
    for patch in inventory():
        target = str(patch["target"])
        if not target.endswith(".go") or target.endswith("_test.go"):
            continue
        projected = ROOT / "vendor/github.com/sagernet/sing-openconnect" / target
        data = projected.read_bytes()
        patch_data = (PACKAGE / str(patch["path"])).read_bytes()
        index = re.search(rb"^index [0-9a-f]{40}\.\.([0-9a-f]{40})", patch_data, re.M)
        actual = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        if index is None or actual != index[1].decode():
            raise RuntimeError(f"Go vendor projection differs: {target}")
        if module is not None and data != (module / target).read_bytes():
            raise RuntimeError(f"replayed module differs from Go vendor: {target}")


def test_vendor() -> None:
    """Run the package's fixture against the tracked product dependencies."""
    verify_vendor(OUTPUT)
    with tempfile.TemporaryDirectory(prefix="h3c-test-overlay-") as directory:
        overlay = Path(directory) / "overlay.json"
        virtual = ROOT / "vendor/github.com/sagernet/sing-openconnect/h3c_test.go"
        overlay.write_text(json.dumps({"Replace": {str(virtual): str(OUTPUT / "h3c_test.go")}}))
        print(run("go", "test", "-mod=vendor", "-overlay=" + str(overlay), "-race", "-count=1", "-run", "TestH3C", "github.com/sagernet/sing-openconnect"), end="")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "check", "test", "verify-vendor"))
    parser.add_argument("--vendor", action="store_true", help="also compare patched production Go files in vendor")
    args = parser.parse_args()
    if args.command == "verify-vendor":
        verify_vendor()
        print(json.dumps(metadata(), indent=2))
        return
    result = prepare(check_only=args.command == "check", check_vendor=args.vendor)
    if args.command == "test":
        test_vendor()
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
