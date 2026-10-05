#!/usr/bin/env python3
"""Render the Homebrew formula and cask for a tagged release into a tap checkout."""

from __future__ import annotations

import argparse
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SHA = re.compile(r"^[0-9a-f]{64}$")


def replace_once(text: str, pattern: str, replacement: str, name: str) -> str:
    new, count = re.subn(pattern, replacement, text, count=1, flags=re.MULTILINE)
    if count != 1:
        raise SystemExit(f"{name}: expected one match for {pattern!r}, got {count}")
    return new


def sha(value: str) -> str:
    if not SHA.match(value):
        raise argparse.ArgumentTypeError(f"not a sha256: {value!r}")
    return value


def main() -> None:
    parser = argparse.ArgumentParser(description="render Homebrew packages for release")
    parser.add_argument("--version", required=True, help="release version without leading v")
    parser.add_argument("--source-sha", required=True, type=sha, help="source tarball sha256")
    parser.add_argument("--arm64-sha", required=True, type=sha, help="darwin arm64 tarball sha256")
    parser.add_argument("--amd64-sha", required=True, type=sha, help="darwin amd64 tarball sha256")
    parser.add_argument("--tap", required=True, type=Path, help="tap checkout to write into")
    args = parser.parse_args()
    if not re.match(r"^[0-9]+\.[0-9]+\.[0-9]+$", args.version):
        raise SystemExit(f"not a release version: {args.version!r}")

    formula = (ROOT / "packaging/homebrew/whatevr.rb").read_text(encoding="utf-8")
    formula = replace_once(
        formula,
        r'^  url "https://github\.com/codelif/whatevr/releases/download/v[^/]+/whatevr-[^"]+\.tar\.gz"$',
        f'  url "https://github.com/codelif/whatevr/releases/download/v{args.version}/whatevr-{args.version}.tar.gz"',
        "formula",
    )
    formula = replace_once(formula, r'^  sha256 "[0-9a-f]{64}"$', f'  sha256 "{args.source_sha}"', "formula")

    cask = (ROOT / "packaging/homebrew/whatevr-cask.rb").read_text(encoding="utf-8")
    cask = replace_once(cask, r'^  version "[^"]+"$', f'  version "{args.version}"', "cask")
    cask = replace_once(cask, r'^  sha256 arm:   "[0-9a-f]{64}",$', f'  sha256 arm:   "{args.arm64_sha}",', "cask")
    cask = replace_once(cask, r'^         intel: "[0-9a-f]{64}"$', f'         intel: "{args.amd64_sha}"', "cask")

    for path, text in [(args.tap / "Formula/whatevr.rb", formula), (args.tap / "Casks/whatevr.rb", cask)]:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        print(path)


if __name__ == "__main__":
    main()
