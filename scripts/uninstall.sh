#!/bin/zsh
# Remove Library Studio KH completely, keeping your settings and library.
# The app bundle, its helper and the login job go; nothing else is touched.
set -e
APP="/Applications/Library Studio KH.app"
SERVER="$APP/Contents/MacOS/library-studio-server"
TRASH="$HOME/.Trash"
stamp=$(date +%Y-%m-%d)

mkdir -p "$TRASH"

if [ -x "$SERVER" ]; then
  "$SERVER" -host-control uninstall || true
fi

# Any loose helper copy an older build left in the config home.
HOME_DIR="${LIBRARY_STUDIO_HOME:-$HOME/Library/Application Support/Library Studio KH}"
if [ -d "$HOME_DIR/Helper" ]; then
  mv "$HOME_DIR/Helper" "$TRASH/Library Studio KH Helper (old copy) $stamp" || true
fi

if [ -d "$APP" ]; then
  mv "$APP" "$TRASH/Library Studio KH.app $stamp"
fi

echo "Library Studio KH removed. Your settings and library are kept."
