#!/usr/bin/env python3
"""Portable, stageable installation. Never activates a user service."""
import os
import shutil
import sys
from pathlib import Path

root = Path(__file__).resolve().parents[1]
profile, prefix, destdir = sys.argv[1:]
prefix = Path(prefix).expanduser().absolute()
def staged(path):
    return Path(destdir) / str(path).lstrip("/") if destdir else path

def install(source, path, mode):
    path = staged(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, path)
    path.chmod(mode)

build = root / "build" / profile
if (build / "whattui").exists():
    install(build / "whattui", prefix / "bin/whattui", 0o755)
    install(root / "packaging/frontends/whattui.json", prefix / "share/whatevr/frontends/whattui.json", 0o644)
if sys.platform == "darwin":
    app = staged(prefix / "Whatevr.app")
    # Avoid merging obsolete bundle contents across versions.
    if app.exists():
        shutil.rmtree(app)
    app.parent.mkdir(parents=True, exist_ok=True)
    shutil.copytree(build / "Whatevr.app", app, symlinks=True)
    # whatevrd lives in the bundle; bin gets a relative link, so a relocated
    # prefix (homebrew's Cellar) keeps pointing at its own copy.
    link = staged(prefix / "bin/whatevrd")
    link.parent.mkdir(parents=True, exist_ok=True)
    if link.is_symlink() or link.exists():
        link.unlink()
    link.symlink_to(os.path.relpath(prefix / "Whatevr.app/Contents/MacOS/whatevrd", prefix / "bin"))
else:
    install(build / "whatevrd", prefix / "bin/whatevrd", 0o755)
    unit = build / "whatevrd.service"
    unit.write_text((root / "packaging/systemd/whatevrd.service.in").read_text().replace("@BINDIR@", str(prefix / "bin")))
    install(unit, prefix / "lib/systemd/user/whatevrd.service", 0o644)
    install(root / "packaging/systemd/whatevrd.socket", prefix / "lib/systemd/user/whatevrd.socket", 0o644)
