# `utreexod`'s Reproducible Build System

This package contains the build script that the `utreexod` project uses in order to
build binaries for each new release. As of `go1.13`, with some new build flags,
binaries are now reproducible, allowing developers to build the binary on
distinct machines, and end up with a byte-for-byte identical binary.
Every release should note which Go version was used to build the release, so
that version should be used for verifying the release.

## Building a New Release

### Tagging and pushing a new tag (for maintainers)

Before running release scripts, a few things need to happen in order to finally
create a release and make sure there are no mistakes in the release process.

First, make sure that before the tagged commit there are modifications to the
[CHANGES](../CHANGES) file committed.
The CHANGES file should be a changelog that roughly mirrors the release notes.
Generally, the PRs that have been merged since the last release have been
listed in the CHANGES file and categorized.
For example, these changes have had the following format in the past:
```
Changes in X.YY.Z (Month Day Year):
  - Protocol and Network-related changes:
    - PR Title One (#PRNUM)
    - PR Title Two (#PRNUMTWO)
    ...
  - RPC changes:
  - Crypto changes:
  ...

  - Contributors (alphabetical order):
    - Contributor A
    - Contributor B
    - Contributor C
    ...
```

If the previous tag is, for example, `vA.B.C`, then you can get the list of
contributors (from `vA.B.C` until the current `HEAD`) using the following command:
```bash
git log vA.B.C..HEAD --pretty="%an" | sort | uniq
```
After committing changes to the CHANGES file, the tagged release commit
should be created.

The tagged commit should be a commit that bumps version numbers in `version.go`
and `cmd/utreexoctl/version.go`.

```diff
diff --git a/cmd/utreexoctl/version.go b/cmd/utreexoctl/version.go
--- a/cmd/utreexoctl/version.go
+++ b/cmd/utreexoctl/version.go
@@ -18,7 +18,7 @@ const semanticAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqr
 const (
 	appMajor uint = 0
 	appMinor uint = 20
-	appPatch uint = 0
+	appPatch uint = 1
 
 	// appPreRelease MUST only contain characters from semanticAlphabet
 	// per the semantic versioning spec.
diff --git a/version.go b/version.go
index 92fd60fdd4..fba55b5a37 100644
--- a/version.go
+++ b/version.go
@@ -18,7 +18,7 @@ const semanticAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqr
 const (
 	appMajor uint = 0
 	appMinor uint = 20
-	appPatch uint = 0
+	appPatch uint = 1
 
 	// appPreRelease MUST only contain characters from semanticAlphabet
 	// per the semantic versioning spec.
```

Next, this commit should be signed by the maintainer using `git commit -S`.
The commit should be tagged and signed with `git tag <TAG> -s`, and should be
pushed using `git push origin TAG`.

### Building a release on macOS/Linux/Windows (WSL)

No prior set up is needed on Linux or macOS is required in order to build the
release binaries. However, on Windows, the only way to build the release
binaries at the moment is by using the Windows Subsystem Linux. One can build
the release binaries following these steps:

1. `git clone https://github.com/utreexo/utreexod.git`
2. `cd utreexod`
3. `./release/release.sh <TAG> # <TAG> is the name of the next release/tag`

This will then create a directory of the form `utreexod-<TAG>` containing archives
of the release binaries for each supported operating system and architecture,
and a manifest file containing the hash of each archive.

### Pushing a release (for maintainers)

Now that the directory `utreexod-<TAG>` is created, the manifest file needs to be
signed by a maintainer and the release files need to be published to GitHub.

Sign the `manifest-<TAG>.txt` file like so:
```sh
gpg --sign --detach-sig manifest-<TAG>.txt
```
This will create a file named `manifest-<TAG>.txt.sig`, which will must
be included in the release files later.

#### Note before publishing
Before publishing, go through the reproducible build process that is outlined
in this document with the files created from `release/release.sh`. This includes
verifying commit and tag signatures using `git verify-commit` and git `verify-tag`
respectively.

Now that we've double-checked everything and have all of the necessary files,
it's time to publish release files on GitHub.
Follow [this documentation](https://docs.github.com/en/github/administering-a-repository/managing-releases-in-a-repository)
to create a release using the GitHub UI, and make sure to write release notes
which roughly follow the format of previous release notes.
This is different from the [CHANGES](../CHANGES) file, which should be before the
tagged commit in the git history.
Much of the information in the release notes will be the same as the CHANGES
file.
It's important to include the Go version used to produce the release files in
the release notes, so users know the correct version of Go to use to reproduce
and verify the build.
When following the GitHub documentation, include every file in the `utreexod-<TAG>`
directory.

At this point, a signed commit and tag on that commit should be pushed to the main
branch. The directory created from running `release/release.sh` should be included
as release files in the GitHub release UI, and the `manifest-<TAG>.txt` file
signature, called `manifest-<TAG>.txt.sig`, should also be included.
A release notes document should be created and written in the GitHub release UI.
Once all of this is done, feel free to click `Publish Release`!

### Building a wallet-enabled release

The wallet-enabled release (`release-with-wallet.sh`) cross-compiles `utreexod`
with the BDK wallet (Rust + CGO) for Linux, Windows, and macOS. It requires
both a Go and Rust toolchain.

#### Building on macOS

macOS builds use the native Xcode toolchain and do not require Nix.

Prerequisites:
- Go
- Rust with targets: `x86_64-apple-darwin`, `aarch64-apple-darwin`
  ```bash
  rustup target add x86_64-apple-darwin aarch64-apple-darwin
  ```
- Xcode Command Line Tools (`xcode-select --install`)

Build:
```bash
./release/release-with-wallet.sh <TAG>
```

Supported targets when building on macOS:
- `darwin-amd64`
- `darwin-arm64`

#### Building on Linux

Linux builds require cross-compilation toolchains for musl/MinGW targets.

The easiest way to get all required toolchains is via the Nix flake at the repo
root, which provides Go, Rust (with cross-compilation targets), and
musl/MinGW cross-compilers:

```bash
nix develop --command ./release/release-with-wallet.sh <TAG>
```

Alternatively, install the toolchains manually:
- Go
- Rust with targets: `x86_64-unknown-linux-musl`, `aarch64-unknown-linux-musl`,
  `arm-unknown-linux-musleabihf`, `armv7-unknown-linux-musleabihf`,
  `x86_64-pc-windows-gnu`, `x86_64-unknown-freebsd`
- musl cross-compilers (see `release/install-musl-deps.sh`)

Supported targets when building on Linux:
- `linux-amd64`
- `linux-arm64`
- `linux-armv6`
- `linux-armv7`
- `windows-amd64`
- `freebsd-amd64`

#### Build options

To build a subset of targets, set `BTCDBUILDSYS`:

```bash
BTCDBUILDSYS="linux-amd64 windows-amd64" ./release/release-with-wallet.sh <TAG>
```

This produces a `utreexod-<TAG>` directory containing archives for each target
with `utreexod` (wallet-enabled) and `utreexoctl`, plus a `manifest-<TAG>.txt`
with SHA256 checksums.

#### Combining Linux and macOS builds

Since the wallet build requires CGO (`CGO_ENABLED=1`), cross-compiling to macOS
from Linux is not possible (Apple's SDK is required). A full release therefore
requires building on both a Linux and a macOS machine, then combining the
results.

1. Build on Linux:
   ```bash
   nix develop --command ./release/release-with-wallet.sh <TAG>
   ```
2. Build on macOS:
   ```bash
   ./release/release-with-wallet.sh <TAG>
   ```
3. Copy both `utreexod-<TAG>` directories to the same machine and combine:
   ```bash
   ./release/combine-releases.sh \
       linux-build/utreexod-<TAG> \
       macos-build/utreexod-<TAG>
   ```

This merges all archives into a single directory and generates a unified
`manifest-<TAG>.txt` with SHA256 checksums of every file.

## Verifying a Release

With `go1.13`, it's now possible for third parties to verify release binaries.
Before this version of `go`, one had to trust the release manager(s) to build the
proper binary. With this new system, third parties can now _independently_ run
the release process, and verify that all the hashes of the release binaries
match exactly that of the release binaries produced by said third parties.

To verify a release, one must obtain the following tools (many of these come
installed by default in most Unix systems): `gpg`/`gpg2`, `shasum`, and
`tar`/`unzip`.

Once done, verifiers can proceed with the following steps:

1. Acquire the archive containing the release binaries for one's specific
   operating system and architecture, and the manifest file along with its
   signature.
2. Verify the signature of the manifest file with `gpg --verify
   manifest-<TAG>.txt.sig`. This will require obtaining the PGP keys which
   signed the manifest file, which are included in the release notes.
3. Recompute the `SHA256` hash of the archive with `shasum -a 256 <filename>`,
   locate the corresponding one in the manifest file, and ensure they match
   __exactly__.

At this point, verifiers can use the release binaries acquired if they trust
the integrity of the release manager(s). Otherwise, one can proceed with the
guide to verify the release binaries were built properly by obtaining `shasum`
and `go` (matching the same version used in the release):

4. Extract the release binaries contained within the archive, compute their
   hashes as done above, and note them down.
5. Ensure `go` is installed, matching the same version as noted in the release
   notes. 
6. Obtain a copy of `utreexod`'s source code with `git clone
   https://github.com/utreexo/utreexod` and checkout the source code of the
   release with `git checkout <TAG>`.
7. Proceed to verify the tag with `git verify-tag <TAG>` and compile the
   binaries from source for the intended operating system and architecture with
   `BTCDBUILDSYS=OS-ARCH ./release/release.sh <TAG>`.
8. Extract the archive found in the `utreexod-<TAG>` directory created by the
   release script and recompute the `SHA256` hash of the release binaries
   (utreexod and utreexoctl) with `shasum -a 256 <filename>`. These should
   match __exactly__ as the ones noted above.