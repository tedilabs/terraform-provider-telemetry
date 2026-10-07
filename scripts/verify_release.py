#!/usr/bin/env python3
"""Verify local GoReleaser output without publishing or making network requests."""

import hashlib
import json
from pathlib import Path
import zipfile


def main():
    dist = Path(__file__).resolve().parents[1] / "dist"
    version = json.loads((dist / "metadata.json").read_text())["version"]
    prefix = f"terraform-provider-telemetry_{version}"
    expected = {f"{prefix}_{os}_{arch}.zip" for os in ("linux", "darwin", "windows") for arch in ("amd64", "arm64")}
    assert {p.name for p in dist.glob("*.zip")} == expected, "unexpected platform archives"
    checksums = {}
    for line in (dist / f"{prefix}_SHA256SUMS").read_text().splitlines():
        digest, name = line.split()
        checksums[name] = digest
    manifest_name = f"{prefix}_manifest.json"
    assert set(checksums) == expected | {manifest_name}, "unexpected checksum entries"
    for name, digest in checksums.items():
        # GoReleaser uses the source manifest in snapshot mode; publishing renames it.
        path = dist / name if name in expected else dist.parent / "terraform-registry-manifest.json"
        assert hashlib.sha256(path.read_bytes()).hexdigest() == digest, name
    manifest = json.loads((dist.parent / "terraform-registry-manifest.json").read_text())
    assert manifest == {"version": 1, "metadata": {"protocol_versions": ["6.0"]}}
    for name in expected:
        suffix = ".exe" if "_windows_" in name else ""
        with zipfile.ZipFile(dist / name) as archive:
            assert set(archive.namelist()) == {"LICENSE", f"terraform-provider-telemetry_v{version}{suffix}"}, name
            assert archive.testzip() is None, name
    print("PASS: six platform ZIPs, binary names, license, protocol 6 manifest, SHA256 checksums")


if __name__ == "__main__":
    main()
