#!/bin/sh
# download-model.sh — fetch a whisper.cpp ggml model into ./models (gitignored).
#
# $0, no API key, no account: pulls from the public Hugging Face mirror
# ggerganov/whisper.cpp (the canonical whisper.cpp model host).
#
# Usage:
#   scripts/download-model.sh            # default: base.en
#   scripts/download-model.sh small.en   # any ggml model name
#   scripts/download-model.sh tiny.en
#
# The downloaded file (models/ggml-<name>.bin) is NEVER committed — see .gitignore.
#
# POSIX sh, macOS-portable: no bash-isms, no GNU-only flags. Uses curl (ships
# with macOS); falls back to wget if curl is absent.

set -eu

MODEL="${1:-base.en}"

# Resolve the repo root from this script's location so it works from anywhere,
# without GNU `readlink -f` (not on stock macOS).
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
MODELS_DIR="$REPO_ROOT/models"

FILE="ggml-${MODEL}.bin"
DEST="$MODELS_DIR/$FILE"
# Public, free, no-auth mirror. resolve/main streams the raw file.
URL="https://huggingface.co/ggerganov/whisper.cpp/resolve/main/$FILE"

mkdir -p "$MODELS_DIR"

if [ -f "$DEST" ]; then
  # Portable mtime read (BSD stat on macOS, GNU stat on Linux).
  if mtime=$(stat -f %m "$DEST" 2>/dev/null); then :; else mtime=$(stat -c %Y "$DEST" 2>/dev/null || echo "?"); fi
  echo "Model already present: $DEST (mtime $mtime)"
  echo "Delete it to re-download."
  exit 0
fi

echo "Downloading whisper model '$MODEL'"
echo "  from: $URL"
echo "  to:   $DEST"

# Download to a temp file first; only move into place on success so a partial
# download never looks like a valid model.
TMP="$DEST.part"
trap 'rm -f "$TMP"' EXIT INT TERM

if command -v curl >/dev/null 2>&1; then
  curl -fL --progress-bar -o "$TMP" "$URL"
elif command -v wget >/dev/null 2>&1; then
  wget -O "$TMP" "$URL"
else
  echo "error: neither curl nor wget is available" >&2
  exit 1
fi

# Sanity: ggml model files are large (tens of MB+). A tiny file means an HTML
# error page slipped through.
SIZE=$(wc -c < "$TMP" | tr -d ' ')
if [ "$SIZE" -lt 1000000 ]; then
  echo "error: downloaded file is only ${SIZE} bytes — likely an error page, not a model" >&2
  exit 1
fi

mv "$TMP" "$DEST"
trap - EXIT INT TERM
echo "Done: $DEST (${SIZE} bytes)"
echo
echo "Build the app with the real engine:"
echo "  go build -tags \"whisper fts5\" ./..."
