#!/bin/bash

# Combines wallet-enabled release directories built on Linux and macOS into a
# single release directory with a unified manifest.
#
# Usage:
#   ./release/combine-releases.sh <linux-dir> <macos-dir>
#
# Example:
#   # Build on Linux:  ./release/release-with-wallet.sh v1.0.0
#   # Build on macOS:  ./release/release-with-wallet.sh v1.0.0
#   # Then combine:
#   ./release/combine-releases.sh \
#       linux-utreexod-v1.0.0/utreexod-v1.0.0 \
#       macos-utreexod-v1.0.0/utreexod-v1.0.0

set -e

if [[ $# -ne 2 ]]; then
    echo "Usage: $0 <linux-dir> <macos-dir>"
    exit 1
fi

LINUX_DIR=$1
MACOS_DIR=$2

if [[ ! -d $LINUX_DIR ]]; then
    echo "Error: Linux release directory '$LINUX_DIR' not found"
    exit 1
fi

if [[ ! -d $MACOS_DIR ]]; then
    echo "Error: macOS release directory '$MACOS_DIR' not found"
    exit 1
fi

# Extract the tag from each directory's manifest filename and verify they match.
LINUX_MANIFEST=$(ls "$LINUX_DIR"/manifest-*.txt 2>/dev/null)
MACOS_MANIFEST=$(ls "$MACOS_DIR"/manifest-*.txt 2>/dev/null)

if [[ -z $LINUX_MANIFEST ]]; then
    echo "Error: no manifest file found in '$LINUX_DIR'"
    exit 1
fi

if [[ -z $MACOS_MANIFEST ]]; then
    echo "Error: no manifest file found in '$MACOS_DIR'"
    exit 1
fi

LINUX_TAG=$(basename "$LINUX_MANIFEST" | sed 's/^manifest-//;s/\.txt$//')
MACOS_TAG=$(basename "$MACOS_MANIFEST" | sed 's/^manifest-//;s/\.txt$//')

if [[ $LINUX_TAG != "$MACOS_TAG" ]]; then
    echo "Error: tag mismatch — Linux='$LINUX_TAG' macOS='$MACOS_TAG'"
    exit 1
fi

TAG=$LINUX_TAG
OUTPUT_DIR="utreexod-$TAG"

mkdir -p "$OUTPUT_DIR"

# Copy all archives and source tarballs from both directories.
# The source archive and vendor tarball will be identical from both builds,
# so the second copy is harmless.
cp "$LINUX_DIR"/*.tar.gz "$OUTPUT_DIR/" 2>/dev/null || true
cp "$LINUX_DIR"/*.zip "$OUTPUT_DIR/" 2>/dev/null || true
cp "$MACOS_DIR"/*.tar.gz "$OUTPUT_DIR/" 2>/dev/null || true
cp "$MACOS_DIR"/*.zip "$OUTPUT_DIR/" 2>/dev/null || true

# Remove any old manifest files copied from the individual builds.
rm -f "$OUTPUT_DIR"/manifest-*.txt

# Generate a unified manifest.
cd "$OUTPUT_DIR"
shasum -a 256 *.tar.gz *.zip > manifest-$TAG.txt 2>/dev/null || true

echo "Combined release in $OUTPUT_DIR:"
cat manifest-$TAG.txt
