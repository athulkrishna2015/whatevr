#!/usr/bin/env python3
# /// script
# requires-python = ">=3.10"
# ///
"""One-command release driver for whatevr.

`just release x.y.z` funnels here. This is the single point where a new
version and its release notes are authored once and propagated everywhere they
are needed (VERSION fallback, AUR packages), then committed and
annotated-tagged. It refuses to run on a dirty tree and never pushes: review the
commit and run `git push --follow-tags` yourself.
"""

from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import NoReturn

VERSION_RE = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$")


def fail(msg: str) -> NoReturn:
    print(f"release: {msg}", file=sys.stderr)
    raise SystemExit(1)


def git(*args: str, capture: bool = True) -> str:
    result = subprocess.run(
        ["git", *args],
        check=True,
        text=True,
        stdout=subprocess.PIPE if capture else None,
    )
    return (result.stdout or "").strip()


def repo_root() -> Path:
    return Path(git("rev-parse", "--show-toplevel"))


def ensure_clean_tree() -> None:
    if git("status", "--porcelain"):
        fail("working tree is not clean: commit or stash changes first")


def ensure_tag_absent(version: str) -> None:
    tag = f"v{version}"
    if subprocess.run(
        ["git", "rev-parse", "-q", "--verify", f"refs/tags/{tag}"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    ).returncode == 0:
        fail(f"tag {tag} already exists")


def capture_notes(args: argparse.Namespace) -> str:
    if args.notes is not None:
        raw = args.notes
    elif args.notes_file is not None:
        raw = Path(args.notes_file).read_text(encoding="utf-8")
    else:
        raw = open_editor(args.version)

    notes = strip_html_comments(raw).strip()
    if not notes:
        fail("release notes are empty, aborting")
    return notes


def open_editor(version: str) -> str:
    editor = os.environ.get("EDITOR") or os.environ.get("VISUAL") or "vi"
    template = (
        "<!--\n"
        f"Release notes for v{version}.\n"
        "Write GitHub-Flavored Markdown; the GitHub release shows it verbatim.\n"
        "Delete these comments or leave them here; they are ignored.\n"
        "Save empty notes to abort.\n"
        "-->\n\n"
    )
    with tempfile.NamedTemporaryFile(
        "w+", suffix=".md", prefix="whatevr-release-", delete=False
    ) as tmp:
        tmp.write(template)
        tmp_path = tmp.name
    try:
        subprocess.run([*editor.split(), tmp_path], check=True)
        return Path(tmp_path).read_text(encoding="utf-8")
    finally:
        os.unlink(tmp_path)


# --- file propagation ------------------------------------------------------


def write_version_file(root: Path, version: str) -> Path:
    path = root / "VERSION"
    path.write_text(version + "\n", encoding="utf-8")
    return path


def update_aur_packages(root: Path, version: str) -> list[Path]:
    changed: list[Path] = []

    source_pkgbuild = root / "packaging/aur/whatevr/PKGBUILD"
    source_srcinfo = root / "packaging/aur/whatevr/.SRCINFO"
    bin_pkgbuild = root / "packaging/aur/whatevr-bin/PKGBUILD"
    bin_srcinfo = root / "packaging/aur/whatevr-bin/.SRCINFO"

    update_lines(
        source_pkgbuild,
        {
            r"^pkgver=.*$": f"pkgver={version}",
            r"^pkgrel=.*$": "pkgrel=1",
        },
    )
    changed.append(source_pkgbuild)

    update_lines(
        source_srcinfo,
        {
            r"^\tpkgver = .*$": f"\tpkgver = {version}",
            r"^\tpkgrel = .*$": "\tpkgrel = 1",
            r"^\tsource = .*$": (
                "\tsource = whatevr-"
                f"{version}.tar.gz::https://github.com/codelif/whatevr/"
                f"releases/download/v{version}/whatevr-{version}.tar.gz"
            ),
        },
    )
    changed.append(source_srcinfo)

    update_lines(
        bin_pkgbuild,
        {
            r"^pkgver=.*$": f"pkgver={version}",
            r"^pkgrel=.*$": "pkgrel=1",
        },
    )
    changed.append(bin_pkgbuild)

    update_lines(
        bin_srcinfo,
        {
            r"^\tpkgver = .*$": f"\tpkgver = {version}",
            r"^\tpkgrel = .*$": "\tpkgrel = 1",
            r"^\tsource_x86_64 = .*$": (
                "\tsource_x86_64 = whatevr-"
                f"{version}-linux-x86_64.tar.zst::https://github.com/codelif/"
                f"whatevr/releases/download/v{version}/"
                f"whatevr-{version}-linux-x86_64.tar.zst"
            ),
        },
    )
    changed.append(bin_srcinfo)

    return changed


def update_lines(path: Path, replacements: dict[str, str]) -> None:
    text = path.read_text(encoding="utf-8")
    for pattern, replacement in replacements.items():
        text, count = re.subn(pattern, replacement, text, count=1, flags=re.MULTILINE)
        if count != 1:
            fail(f"{path}: expected one match for {pattern!r}, got {count}")
    path.write_text(text, encoding="utf-8")


# --- release note formatting ----------------------------------------------


HTML_COMMENT_RE = re.compile(r"<!--.*?-->", re.DOTALL)


def strip_html_comments(markdown: str) -> str:
    return HTML_COMMENT_RE.sub("", markdown)


# --- main ------------------------------------------------------------------


def main() -> None:
    parser = argparse.ArgumentParser(description="cut a whatevr release")
    parser.add_argument("version", help="semantic version, e.g. 0.2.0")
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--notes", help="Markdown release notes")
    group.add_argument("--notes-file", help="read release notes from a file")
    args = parser.parse_args()

    version = args.version.lstrip("v")
    args.version = version
    if not VERSION_RE.match(version):
        fail(f"invalid version {version!r}; expected X.Y.Z[-suffix]")

    root = repo_root()
    os.chdir(root)
    ensure_clean_tree()
    ensure_tag_absent(version)

    notes = capture_notes(args)

    changed = [
        write_version_file(root, version),
        *update_aur_packages(root, version),
    ]

    rel = [str(p.relative_to(root)) for p in changed]
    git("add", *rel, capture=False)
    git("commit", "-m", f"version: {version}", capture=False)

    tag = f"v{version}"
    # Pass the notes through a file with --cleanup=verbatim so Markdown survives
    # byte-for-byte. The default `git tag -m` cleanup strips every line starting
    # with '#', which would silently delete headings before they reach the
    # GitHub release (the workflow reads them back from the annotated tag body).
    tag_message = f"{tag}\n\n{notes}\n"
    with tempfile.NamedTemporaryFile(
        "w", suffix=".txt", prefix="whatevr-tag-", delete=False, encoding="utf-8"
    ) as tag_file:
        tag_file.write(tag_message)
        tag_msg_path = tag_file.name
    try:
        subprocess.run(
            ["git", "tag", "-a", tag, "--cleanup=verbatim", "-F", tag_msg_path],
            check=True,
        )
    finally:
        os.unlink(tag_msg_path)

    print()
    print(f"Tagged {tag}. Nothing has been pushed.")
    print("Next:  git push --follow-tags")


if __name__ == "__main__":
    main()
