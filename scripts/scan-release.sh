#!/bin/zsh
# Release gate: scan the built DMG's own bytes before publish. A PASS over an
# unbuilt or unclean artifact is how a tailnet IP and host strings reached Hub's
# public page (issues row 20261120-138). Judge the artifact by its contents,
# never by rebuilding it — three builds of one commit give three hashes.
#
#   scripts/scan-release.sh <dmg>
#
# The shared scanner lives in kh-mirror-tools and is used as-is; this wrapper
# only refuses a missing/unsigned input and keeps the identity and exit status
# with the run. A non-zero exit blocks publish.
set -u
[[ -n "${ZSH_VERSION:-}" ]] || { echo "REFUSED: run with zsh so the shared scanner's builtins run"; exit 2; }
dmg=${1:?usage: scan-release.sh <dmg>}
tools=${KH_MIRROR_TOOLS:-/home/user/Developer/_orchestra/developer-handoff/kh-mirror-tools}
scan="$tools/scan-dmg.sh"
[ -x "$scan" ] || { echo "REFUSED: $scan is missing — nothing would be scanned"; exit 2; }
[ -f "$dmg" ] || { echo "REFUSED: $dmg does not exist — build before scanning"; exit 2; }

echo "== scan-release identity"
shasum -a 256 "$dmg"
stat -f '%z bytes' "$dmg"
hdiutil verify "$dmg" || { echo "REFUSED: hdiutil verify failed — the image is damaged"; exit 4; }

echo "== scan-release contents"
/bin/zsh "$scan" "$dmg"
rc=$?
echo "SCAN_EXIT=$rc"
[ $rc -eq 0 ] || echo "REFUSED: the artifact is not clean; publish stops here"
exit $rc
