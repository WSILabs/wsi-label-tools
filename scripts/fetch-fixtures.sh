#!/usr/bin/env bash
# Populate ./sample_files from the wsilabs/wsi-fixtures v2 release (CI parity).
# Requires gh auth. Alternatively symlink a local opentile-go/sample_files.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p sample_files && cd sample_files
gh release download v2 --repo wsilabs/wsi-fixtures --pattern '*.tar'
for t in *.tar; do tar xf "$t" && rm "$t"; done
ls -R .
