import argparse
import hashlib
import json
from pathlib import Path
import plistlib
import shutil
import subprocess
import tomllib


def run(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--platform", choices=["windows", "macos"], required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    standalone = Path(__file__).resolve().parents[1]
    version = tomllib.loads((standalone / "Cargo.toml").read_text(encoding="utf-8"))["package"]["version"]
    args.output.mkdir(parents=True, exist_ok=True)
    name = f"IKUN-Bridge-{version}"
    if args.platform == "windows":
        artifact = args.output / f"{name}-Windows-x64.exe"
        shutil.copy2(args.binary, artifact)
    else:
        architectures = subprocess.check_output(["lipo", "-archs", str(args.binary)], text=True).split()
        assert set(architectures) == {"x86_64", "arm64"}, "Universal binary required"
        app = args.output / "IKUN Bridge.app"
        contents = app / "Contents"
        (contents / "MacOS").mkdir(parents=True)
        (contents / "Resources").mkdir()
        executable = contents / "MacOS" / "IKUNBridge"
        shutil.copy2(args.binary, executable)
        executable.chmod(0o755)
        info = plistlib.loads((standalone / "tools" / "Info.plist").read_bytes())
        info["CFBundleShortVersionString"] = version
        major, minor, patch = map(int, version.split("."))
        info["CFBundleVersion"] = str(major * 10000 + minor * 100 + patch)
        (contents / "Info.plist").write_bytes(plistlib.dumps(info, sort_keys=False))
        shutil.copy2(standalone.parent / "src-tauri" / "icons" / "icon.icns", contents / "Resources" / "Bridge.icns")
        shutil.copy2(standalone / "ui" / "Lucide-LICENSE.txt", contents / "Resources" / "Lucide-LICENSE.txt")
        run(["codesign", "--force", "--sign", "-", "--timestamp=none", str(app)])
        run(["codesign", "--verify", "--deep", "--strict", str(app)])
        artifact = args.output / f"{name}-macOS-universal.zip"
        run(["ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", str(app), str(artifact)])
    result = subprocess.run(["git", "rev-parse", "HEAD"], cwd=standalone, capture_output=True, text=True)
    commit = result.stdout.strip() if result.returncode == 0 else "unknown"
    with artifact.open("rb") as stream:
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
    (args.output / f"checksums-{args.platform}.txt").write_text(f"{digest}  {artifact.name}\n", encoding="utf-8")
    manifest = {"version": version, "commit": commit, "platform": args.platform, "file": artifact.name, "sha256": digest, "size": artifact.stat().st_size}
    (args.output / f"build-info-{args.platform}.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(manifest))


if __name__ == "__main__":
    main()
