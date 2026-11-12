#!/usr/bin/env python3
"""Retain the accepted dist pair and rotate older installers automatically.

Calls the shared portfolio helper (release-dist.py) with the pin document, then
moves any remaining Library DMG older than the newest two into this project's
`_trash/<date> <what>/`, so keeping the latest two never needs a hand step.

Guards: a version named by the live or previous feed release, the dist ledger,
artifact retention, rollback/state records, a queued publish job or the
`base/versions` runtimes is never moved. Every move is logged with its hash.
"""

import argparse
import base64
import datetime
import glob
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys

VERSION_RE = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
HELPER = os.path.expanduser("~/Developer/Shared Tools/portfolio/tools/release-dist.py")
UPDATER = os.path.expanduser("~/Library/KHUpdater")
BUNDLE_ID = "app.librarystudiokh.desktop"
APP_NAME = "Library Studio KH"


def version_of(name, prefix):
    if not (name.startswith(prefix) and name.endswith(".dmg")):
        return None
    middle = name[len(prefix):-len(".dmg")]
    if middle.startswith("-"):
        middle = middle[1:]
    return tuple(int(part) for part in middle.split(".")) if VERSION_RE.match(middle) else None


def dotted(version):
    return ".".join(str(part) for part in version)


def feed_versions(path, protected, reason):
    """Read a signed release file and protect this app's versions from it."""
    try:
        with open(path) as handle:
            release = json.load(handle)
        body = json.loads(base64.b64decode(release["feed"]))
    except Exception:
        return
    for entry in (body.get("apps") or {}).values():
        if entry.get("bundleID") == BUNDLE_ID and VERSION_RE.match(str(entry.get("version", ""))):
            protected.setdefault(entry["version"], reason)


def json_versions(path, protected, reason):
    """Protect any dotted version named anywhere in a small state document."""
    try:
        text = open(path).read()
    except OSError:
        return
    for version in re.findall(r"\b\d{1,9}\.\d{1,9}\.\d{1,9}\b", text):
        protected.setdefault(version, reason)


def protected_versions(dist):
    protected = {}
    ledger = os.path.join(dist, ".release-dist.json")
    if os.path.isfile(ledger):
        try:
            for entry in (json.load(open(ledger)).get("apps") or {}).values():
                for meta in (entry.get("files") or {}).values():
                    if VERSION_RE.match(str(meta.get("version", ""))):
                        protected.setdefault(meta["version"], "dist ledger")
        except Exception:
            pass
    for name in ("release.json", "previous-release.json"):
        feed_versions(os.path.join(UPDATER, "feed", name), protected, "feed " + name)
    json_versions(os.path.join(UPDATER, "feed", "artifact-retention.json"), protected, "artifact retention")
    for path in glob.glob(os.path.join(UPDATER, "state", "*.json")) + glob.glob(os.path.join(UPDATER, "state", "rollback-*")):
        json_versions(path, protected, "updater state")
    versions_dir = os.path.join(UPDATER, "base", "versions")
    if os.path.isdir(versions_dir):
        for name in os.listdir(versions_dir):
            if VERSION_RE.match(name):
                protected.setdefault(name, "base/versions runtime")
    for path in glob.glob(os.path.expanduser("~/Developer/_orchestra/updater-lead/*/publish-queue.md")):
        try:
            for line in open(path):
                if APP_NAME in line:
                    for version in re.findall(r"\b\d{1,9}\.\d{1,9}\.\d{1,9}\b", line):
                        protected.setdefault(version, "queued publish job")
        except OSError:
            pass
    return protected


def unique_target(trash, name):
    stem, suffix = os.path.splitext(name)
    target, number = os.path.join(trash, name), 2
    while os.path.lexists(target):
        target = os.path.join(trash, f"{stem}-{number}{suffix}")
        number += 1
    return target


def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def default_trash(project, today=None):
    day = today or datetime.date.today().isoformat()
    return os.path.join(project, "_trash", f"{day} library dist rotation")


def rotate(dist, trash, prefix, keep, protected=None):
    protected = protected or {}
    found = []
    for name in os.listdir(dist):
        version = version_of(name, prefix)
        if version is not None:
            found.append((version, name))
    found.sort(reverse=True)
    moved, skipped = [], []
    for version, name in found[keep:]:
        entry = {"version": dotted(version), "file": name}
        reason = protected.get(dotted(version))
        if reason:
            entry["kept_because"] = reason
            skipped.append(entry)
            continue
        source = os.path.join(dist, name)
        entry.update({"sha256": sha256(source), "bytes": os.path.getsize(source)})
        os.makedirs(trash, exist_ok=True)
        target = unique_target(trash, name)
        shutil.move(source, target)
        entry.update({"from": source, "to": target})
        moved.append(entry)
    return {"kept": [name for _, name in found[:keep]], "moved": moved, "skipped": skipped}


def main():
    project = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    parser = argparse.ArgumentParser()
    parser.add_argument("pins", nargs="?", help="pin document for the shared release-dist helper")
    parser.add_argument("--rotate-only", action="store_true", help="skip the helper and only rotate dist")
    parser.add_argument("--dist", default=os.path.join(project, "dist"))
    parser.add_argument("--project", default=project, help="project whose _trash receives retired installers")
    parser.add_argument("--trash", default=None, help="default: <project>/_trash/<date> library dist rotation")
    parser.add_argument("--prefix", default=APP_NAME)
    parser.add_argument("--keep", type=int, default=2)
    args = parser.parse_args()

    helper = {}
    if not args.rotate_only:
        if not args.pins:
            sys.exit("release-retain: a pin document is required unless --rotate-only is given")
        if not os.path.isfile(HELPER):
            sys.exit(f"release-retain: the shared helper is missing: {HELPER}")
        result = subprocess.run([sys.executable, "-I", HELPER, args.pins], capture_output=True, text=True)
        if result.returncode != 0:
            sys.stderr.write(result.stdout + result.stderr)
            sys.exit(result.returncode)
        helper = json.loads(result.stdout) if result.stdout.strip() else {}
    trash = args.trash or default_trash(args.project)
    rotation = rotate(args.dist, trash, args.prefix, args.keep, protected_versions(args.dist))
    rotation["trash"] = trash
    print(json.dumps({"helper": helper, "rotation": rotation}, indent=2))


if __name__ == "__main__":
    main()
