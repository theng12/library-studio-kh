#!/usr/bin/env python3
"""Retain the accepted dist pair and rotate older installers automatically.

Calls the shared portfolio helper (release-dist.py) with the pin document, then
moves any remaining Library DMG older than the newest two straight to the macOS
Trash, so keeping the latest two never needs a hand step. The helper still owns
the ledger; this only finishes the rotation it deliberately leaves alone.
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import sys

VERSION_RE = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
HELPER = os.path.expanduser("~/Developer/Shared Tools/portfolio/tools/release-dist.py")


def version_of(name, prefix):
    if not (name.startswith(prefix) and name.endswith(".dmg")):
        return None
    middle = name[len(prefix):-len(".dmg")]
    if middle.startswith("-"):
        middle = middle[1:]
    return tuple(int(part) for part in middle.split(".")) if VERSION_RE.match(middle) else None


def unique_target(trash, name):
    stem, suffix = os.path.splitext(name)
    target, number = os.path.join(trash, name), 2
    while os.path.lexists(target):
        target = os.path.join(trash, f"{stem}-{number}{suffix}")
        number += 1
    return target


def rotate(dist, trash, prefix, keep):
    os.makedirs(trash, exist_ok=True)
    found = []
    for name in os.listdir(dist):
        version = version_of(name, prefix)
        if version is not None:
            found.append((version, name))
    found.sort(reverse=True)
    moved = []
    for _, name in found[keep:]:
        source = os.path.join(dist, name)
        target = unique_target(trash, name)
        shutil.move(source, target)
        moved.append({"from": source, "to": target})
    return {"kept": [name for _, name in found[:keep]], "moved": moved}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("pins", nargs="?", help="pin document for the shared release-dist helper")
    parser.add_argument("--rotate-only", action="store_true", help="skip the helper and only rotate dist")
    parser.add_argument("--dist", default=os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "dist"))
    parser.add_argument("--trash", default=os.path.expanduser("~/.Trash"))
    parser.add_argument("--prefix", default="Library Studio KH")
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
    rotation = rotate(args.dist, args.trash, args.prefix, args.keep)
    print(json.dumps({"helper": helper, "rotation": rotation}, indent=2))


if __name__ == "__main__":
    main()
