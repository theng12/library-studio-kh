#!/bin/sh
# Write docs/appcast.xml for one final, published DMG, signed with this app's own EdDSA key.
#
#   scripts/make-appcast.sh <version> <dmg> <download-url> [release-notes]
#
# Run only after the DMG is final: a re-cut DMG must regenerate the item, because a stale
# signature fails silently. The key is never in the repo; it lives at
# ~/.kh-sparkle/app.librarystudiokh.desktop/private.key (0600) and must never be regenerated
# while an install trusts it.
set -eu
VERSION=${1:?version}
DMG=${2:?dmg}
URL=${3:?download url}
NOTES=${4:-}
KEY="$HOME/.kh-sparkle/app.librarystudiokh.desktop/private.key"
SIGN=/Users/vengtheng/Developer/_orchestra/developer-system/sparkle-shim-20261123/vendor/bin/sign_update
OUT=${APPCAST_OUT:-docs/appcast.xml}
MIN=13.0

[ -f "$KEY" ] || { echo "no EdDSA key at $KEY" >&2; exit 1; }
[ -f "$DMG" ] || { echo "no DMG at $DMG" >&2; exit 1; }
SIG=$("$SIGN" --ed-key-file "$KEY" -p "$DMG")
LEN=$(stat -f %z "$DMG")
DATE=$(date -u '+%a, %d %b %Y %H:%M:%S +0000')
mkdir -p docs

cat > "$OUT" <<XML
<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
  <channel>
    <title>Library Studio KH</title>
    <link>https://theng12.github.io/library-studio-kh/appcast.xml</link>
    <description>Library Studio KH updates</description>
    <language>en</language>
    <item>
      <title>Version $VERSION</title>
      <pubDate>$DATE</pubDate>
      <sparkle:version>$VERSION</sparkle:version>
      <sparkle:shortVersionString>$VERSION</sparkle:shortVersionString>
      <sparkle:minimumSystemVersion>$MIN</sparkle:minimumSystemVersion>
      <description><![CDATA[$NOTES]]></description>
      <enclosure url="$URL" sparkle:edSignature="$SIG" length="$LEN" type="application/octet-stream"/>
    </item>
  </channel>
</rss>
XML
echo "wrote $OUT for $VERSION ($LEN bytes)"
