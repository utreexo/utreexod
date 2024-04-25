#!/bin/bash

# Copyright (c) 2016 Company 0, LLC.
# Copyright (c) 2016-2020 The btcsuite developers
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.

# Simple bash script to build basic utreexod tools for all the platforms we support
# with the golang cross-compiler.

set -e

# If no tag specified, use date + version otherwise use tag.
if [[ $1x = x ]]; then
    DATE=`date +%Y%m%d`
    VERSION="01"
    TAG=$DATE-$VERSION
else
    TAG=$1
fi

go mod vendor
tar -cvzf vendor.tar.gz vendor

PACKAGE=utreexod
MAINDIR=$PACKAGE-$TAG
mkdir -p $MAINDIR

cp vendor.tar.gz $MAINDIR/
rm vendor.tar.gz
rm -r vendor

PACKAGESRC="$MAINDIR/$PACKAGE-source-$TAG.tar"
git archive -o $PACKAGESRC HEAD
gzip -f $PACKAGESRC > "$PACKAGESRC.gz"

cd $MAINDIR

SYS=

if [[ $(uname) == Linux ]]; then
  # If BTCDBUILDSYS is set the default list is ignored. Useful to release
  # for a subset of systems/architectures.
  SYS=${BTCDBUILDSYS:-"
          linux-armv6
          linux-armv7
          linux-arm64
          linux-amd64
          windows-amd64
  "}
elif [[ $(uname) == Darwin ]]; then
  # If BTCDBUILDSYS is set the default list is ignored. Useful to release
  # for a subset of systems/architectures.
  SYS=${BTCDBUILDSYS:-"
          darwin-amd64
          darwin-arm64
  "}
fi

CPUCORES=
if [[ $(uname) == Linux ]]; then
  CPUCORES=$(lscpu | grep "Core(s) per socket" | awk '{print $4}')
elif [[ $(uname) == Darwin ]]; then
  CPUCORES=$(sysctl -n hw.physicalcpu)
fi

# Use the first element of $GOPATH in the case where GOPATH is a list
# (something that is totally allowed).
PKG="github.com/utreexo/utreexod"
COMMIT=$(git describe --tags --abbrev=40 --dirty)

for i in $SYS; do
    OS=$(echo $i | cut -f1 -d-)
    ARCH=$(echo $i | cut -f2 -d-)
    ARM=
    CC=

    if [[ $ARCH = "armv6" ]]; then
      ARCH=arm
      ARM=6
    elif [[ $ARCH = "armv7" ]]; then
      ARCH=arm
      ARM=7
    fi

    mkdir $PACKAGE-$i-$TAG
    echo "cd to" $PACKAGE-$i-$TAG
    cd $PACKAGE-$i-$TAG

    # Build bdk wallet(rust) dependency.
    if [[ $ARCH = "arm64" ]] && [[ $OS = "linux" ]]; then
      TARGET="aarch64-unknown-linux-musl"
      CC=aarch64-linux-musl-gcc

    elif [[ $ARCH = "amd64" ]] && [[ $OS = "linux" ]]; then
      TARGET="x86_64-unknown-linux-musl"
      CC=x86_64-linux-musl-gcc

    elif [[ $ARCH = "arm" ]] && [[ $OS = "linux" ]] && [[ $ARM=7 ]]; then
      TARGET="armv7-unknown-linux-musleabihf"
      CC=armv7l-linux-musleabihf-gcc

    elif [[ $ARCH = "arm" ]] && [[ $OS = "linux" ]] && [[ $ARM=6 ]]; then
      TARGET="armv6-unknown-linux-musleabihf"
      CC=armv6-linux-musleabihf-gcc

    elif [[ $ARCH = "arm64" ]] && [[ $OS = "darwin" ]]; then
      TARGET="aarch64-apple-darwin"

    elif [[ $ARCH = "amd64" ]] && [[ $OS = "darwin" ]]; then
      TARGET="x86_64-apple-darwin"

    elif [[ $ARCH = "amd64" ]] && [[ $OS = "windows" ]]; then
      TARGET="x86_64-pc-windows-gnu"
      CC=x86_64-w64-mingw32-gcc
    fi

    env CARGO_TARGET_DIR=. cargo build --release --target=$TARGET --jobs=$CPUCORES
    mv $TARGET target

    echo "Building:" $OS $ARCH $ARM

    # Build utreexod
    if [[ $OS == "linux" ]]; then
      env CC=$CC CGO_ENABLED=1 GOOS=$OS GOARCH=$ARCH GOARM=$ARM go build -v -tags="bdkwallet" -ldflags="-s -w -extldflags "-static" -buildid=" github.com/utreexo/utreexod
    elif [[ $OS == "darwin" ]]; then
      env CGO_ENABLED=1 GOOS=$OS GOARCH=$ARCH GOARM=$ARM go build -v -tags="bdkwallet" -ldflags="-s -w -buildid=" github.com/utreexo/utreexod
    fi

    # Remove bdk build things.
    rm -rf target
    rm -rf release

    # Build utreexoctl
    env CGO_ENABLED=0 GOOS=$OS GOARCH=$ARCH GOARM=$ARM go build -v -trimpath -ldflags="-s -w -buildid=" github.com/utreexo/utreexod/cmd/utreexoctl

    cd ..

    if [[ $OS = "windows" ]]; then
	zip -r $PACKAGE-$i-$TAG.zip $PACKAGE-$i-$TAG
    else
	tar -cvzf $PACKAGE-$i-$TAG.tar.gz $PACKAGE-$i-$TAG
    fi

    rm -r $PACKAGE-$i-$TAG
done

shasum -a 256 * > manifest-$TAG.txt
