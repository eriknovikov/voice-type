#!/bin/sh
# Compatibility redirect. This path was the published one-liner before the tree
# moved into v5/, so it forwards there and keeps older links working:
#   curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | sh
# New installs should target a version directly -- see README.md.
set -eu

_dir=$(dirname "$0")
if [ -f "$_dir/v5/install.sh" ]; then
    exec sh "$_dir/v5/install.sh" "$@"
fi
curl -fsSL "https://raw.githubusercontent.com/eriknovikov/voice-type/main/v5/install.sh" | sh -s -- "$@"
