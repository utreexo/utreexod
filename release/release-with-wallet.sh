#!/bin/bash

set -eo pipefail

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

# Save repo root so cargo and go can find source files after we cd.
REPOROOT="$(git rev-parse --show-toplevel)"

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
          freebsd-amd64
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

for i in $SYS; do
    OS=$(echo $i | cut -f1 -d-)
    ARCH=$(echo $i | cut -f2 -d-)
    ARM=
    CC=
    CARGO_ENV=

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

    # Set the Rust target triple, the C cross-compiler (CC), and cargo
    # environment variables.  Cargo needs two things for cross-compilation:
    #   CARGO_TARGET_<TRIPLE>_LINKER  – which linker to invoke
    #   CC_<triple>                   – which C compiler the `cc` crate uses
    #                                   to compile vendored C code (e.g. secp256k1)
    # CFLAGS_<triple> disables _FORTIFY_SOURCE which is incompatible with musl
    # static linking (some musl cross-compilers ship fortify-headers that
    # reference symbols like __memcpy_chk which musl libc does not provide).
    if [[ $ARCH = "arm64" ]] && [[ $OS = "linux" ]]; then
      TARGET="aarch64-unknown-linux-musl"
      CC=aarch64-linux-musl-gcc
      CARGO_ENV="CARGO_TARGET_AARCH64_UNKNOWN_LINUX_MUSL_LINKER=aarch64-linux-musl-gcc CC_aarch64_unknown_linux_musl=aarch64-linux-musl-gcc CFLAGS_aarch64_unknown_linux_musl=-U_FORTIFY_SOURCE"

    elif [[ $ARCH = "amd64" ]] && [[ $OS = "linux" ]]; then
      TARGET="x86_64-unknown-linux-musl"
      CC=x86_64-linux-musl-gcc
      CARGO_ENV="CARGO_TARGET_X86_64_UNKNOWN_LINUX_MUSL_LINKER=x86_64-linux-musl-gcc CC_x86_64_unknown_linux_musl=x86_64-linux-musl-gcc CFLAGS_x86_64_unknown_linux_musl=-U_FORTIFY_SOURCE"

    elif [[ $ARCH = "arm" ]] && [[ $OS = "linux" ]] && [[ $ARM = 7 ]]; then
      TARGET="armv7-unknown-linux-musleabihf"
      CC=armv7l-linux-musleabihf-gcc
      CARGO_ENV="CARGO_TARGET_ARMV7_UNKNOWN_LINUX_MUSLEABIHF_LINKER=armv7l-linux-musleabihf-gcc CC_armv7_unknown_linux_musleabihf=armv7l-linux-musleabihf-gcc CFLAGS_armv7_unknown_linux_musleabihf=-U_FORTIFY_SOURCE"

    elif [[ $ARCH = "arm" ]] && [[ $OS = "linux" ]] && [[ $ARM = 6 ]]; then
      TARGET="arm-unknown-linux-musleabihf"
      CC=armv6-linux-musleabihf-gcc
      CARGO_ENV="CARGO_TARGET_ARM_UNKNOWN_LINUX_MUSLEABIHF_LINKER=armv6-linux-musleabihf-gcc CC_arm_unknown_linux_musleabihf=armv6-linux-musleabihf-gcc CFLAGS_arm_unknown_linux_musleabihf=-U_FORTIFY_SOURCE"

    elif [[ $ARCH = "arm64" ]] && [[ $OS = "darwin" ]]; then
      TARGET="aarch64-apple-darwin"
      # macOS builds use the system Xcode toolchain (not Nix) for universal
      # binary support. Cross-compiling requires the SDK with universal libs.
      SDKROOT=$(xcrun --sdk macosx --show-sdk-path)
      if [[ $(uname -m) != arm64 ]]; then
        CC="clang -arch arm64 -isysroot $SDKROOT"
      fi

    elif [[ $ARCH = "amd64" ]] && [[ $OS = "darwin" ]]; then
      TARGET="x86_64-apple-darwin"
      # macOS builds use the system Xcode toolchain (not Nix) for universal
      # binary support. Cross-compiling requires the SDK with universal libs.
      SDKROOT=$(xcrun --sdk macosx --show-sdk-path)
      if [[ $(uname -m) != x86_64 ]]; then
        CC="clang -arch x86_64 -isysroot $SDKROOT"
      fi

    elif [[ $ARCH = "amd64" ]] && [[ $OS = "windows" ]]; then
      TARGET="x86_64-pc-windows-gnu"
      CC=x86_64-w64-mingw32-gcc
      CARGO_ENV="CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER=x86_64-w64-mingw32-gcc CC_x86_64_pc_windows_gnu=x86_64-w64-mingw32-gcc"

    elif [[ $ARCH = "amd64" ]] && [[ $OS = "freebsd" ]]; then
      TARGET="x86_64-unknown-freebsd"
      CC=x86_64-freebsd-cc
      CARGO_ENV="CARGO_TARGET_X86_64_UNKNOWN_FREEBSD_LINKER=x86_64-freebsd-cc CC_x86_64_unknown_freebsd=x86_64-freebsd-cc"

    fi

    # Build the Rust static library (libbdkgo.a).
    # The workspace Cargo.toml at the repo root causes cargo to output to
    # <repo>/target/<triple>/release/libbdkgo.a.  The CGO LDFLAGS directive
    # in bdkwallet.go references ${SRCDIR}/../target/release/libbdkgo.a
    # (Go expands ${SRCDIR} to the bdkwallet/ directory, so this resolves to
    # <repo>/target/release/libbdkgo.a).  We copy the library there after
    # the cargo build.
    # Pass CC to cargo when set so the cc crate uses the right compiler.
    # CC="$CC" is used as a prefix assignment (not through CARGO_ENV) because
    # Darwin cross-compile values contain spaces (e.g. "clang -arch x86_64 ...").
    if [[ -n "$CC" ]]; then
      CC="$CC" env $CARGO_ENV \
        cargo build --release \
        --manifest-path="$REPOROOT/bdkwallet/bdkgo_crate/Cargo.toml" \
        --target=$TARGET --jobs=$CPUCORES
    else
      env $CARGO_ENV \
        cargo build --release \
        --manifest-path="$REPOROOT/bdkwallet/bdkgo_crate/Cargo.toml" \
        --target=$TARGET --jobs=$CPUCORES
    fi

    mkdir -p "$REPOROOT/target/release"
    cp "$REPOROOT/target/$TARGET/release/libbdkgo.a" \
      "$REPOROOT/target/release/"

    echo "Building:" $OS $ARCH $ARM

    # Build utreexod
    if [[ $OS = "linux" ]]; then
      env CC=$CC CGO_ENABLED=1 GOOS=$OS GOARCH=$ARCH GOARM=$ARM go build -v -tags="bdkwallet" -ldflags='-s -w -extldflags "-static" -buildid=' github.com/utreexo/utreexod
    elif [[ $OS = "darwin" ]]; then
      env CC="${CC:-clang}" CGO_ENABLED=1 GOOS=$OS GOARCH=$ARCH GOARM=$ARM go build -v -tags="bdkwallet" -ldflags='-s -w -buildid=' github.com/utreexo/utreexod
    elif [[ $OS = "windows" ]]; then
      env CC=$CC CGO_ENABLED=1 GOOS=$OS GOARCH=$ARCH go build -v -tags="bdkwallet" -ldflags='-s -w -extldflags "-static" -buildid=' github.com/utreexo/utreexod
    elif [[ $OS = "freebsd" ]]; then
      env CC=$CC CGO_ENABLED=1 GOOS=$OS GOARCH=$ARCH go build -v -tags="bdkwallet" -ldflags='-s -w -buildid=' github.com/utreexo/utreexod
    fi

    # Remove the copied library (target-specific artifacts under target/<triple>/
    # are kept so that proc-macros and build scripts don't need to be rebuilt
    # for the next target).
    rm -f "$REPOROOT/target/release/libbdkgo.a"

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

# Clean up cargo build cache.
rm -rf "$REPOROOT/target"

shasum -a 256 * > manifest-$TAG.txt
