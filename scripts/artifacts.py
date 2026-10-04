#!/usr/bin/env python3
"""Create source and platform-native binary archives without GNU tar options."""
import hashlib
import io
import subprocess
import sys
import tarfile
from pathlib import Path

root = Path(__file__).resolve().parents[1]
build = root / "build"
build.mkdir(exist_ok=True)
version = subprocess.check_output([str(root / "scripts/version.py"), "full"], text=True).strip()
if sys.argv[1] == "source":
    destination = build / f"whatevr-{version}.tar.gz"
    with tarfile.open(destination, "w:gz") as output:
        for directory, prefix in [(root, ""), (root / "whattui/vaxis", "whattui/vaxis/"), (root / "whatevrd/whatsmeow", "whatevrd/whatsmeow/")]:
            archive = subprocess.check_output(["git", "-C", str(directory), "archive", "--format=tar", "HEAD"])
            with tarfile.open(fileobj=io.BytesIO(archive)) as source:
                for member in source:
                    if not prefix and member.name == "VERSION":
                        continue
                    data = source.extractfile(member) if member.isfile() else None
                    member.name = f"whatevr-{version}/{prefix}{member.name}"
                    output.addfile(member, data)
        data = (version + "\n").encode()
        info = tarfile.TarInfo(f"whatevr-{version}/VERSION")
        info.size, info.mode = len(data), 0o644
        output.addfile(info, io.BytesIO(data))
    print(destination)
elif sys.argv[1] == "darwin":
    arch = {"x86_64": "amd64"}.get(sys.argv[2], sys.argv[2])
    expected = {"amd64": "x86_64", "arm64": "arm64"}.get(arch)
    if not expected:
        raise SystemExit("Darwin artifacts require arm64 or amd64")
    for executable in [build / "release/whatevrd", build / "release/whattui", build / "release/Whatevr Notifications.app/Contents/MacOS/WhatevrNotifications"]:
        actual = subprocess.check_output(["lipo", "-archs", str(executable)], text=True).strip()
        if actual != expected:
            raise SystemExit(f"{executable.name} is {actual}, not {expected}; build on the matching architecture")
    name = f"whatevr-{version}-darwin-{arch}"
    destination = build / (name + ".tar.gz")
    with tarfile.open(destination, "w:gz") as archive:
        for filename in ["whatevrd", "whattui", "Whatevr Notifications.app"]:
            archive.add(build / "release" / filename, arcname=f"{name}/{filename}")
        for filename in ["LICENSE", "README.md"]:
            archive.add(root / filename, arcname=f"{name}/{filename}")
    print(destination)
elif sys.argv[1] == "checksums":
    with (build / "SHA256SUMS").open("w") as output:
        for artifact in sorted([*build.glob("whatevr-*.tar.gz"), *build.glob("whatevr-*.tar.zst")]):
            with artifact.open("rb") as stream:
                digest = hashlib.file_digest(stream, "sha256").hexdigest()
            output.write(digest + "  " + artifact.name + "\n")
else:
    raise SystemExit("unknown artifact command")
