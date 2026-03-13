{
  description = "utreexod development and cross-compilation environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    rust-overlay = {
      url = "github:oxalica/rust-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, rust-overlay }:
    let
      supportedSystems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs supportedSystems f;
    in
    {
      devShells = forAllSystems (system:
        let
          pkgs = import nixpkgs {
            inherit system;
            overlays = [ rust-overlay.overlays.default ];
          };
          isLinux = pkgs.stdenv.isLinux;

          # Rust toolchain with cross-compilation target std libraries.
          rustToolchain = pkgs.rust-bin.stable.latest.default.override {
            targets =
              pkgs.lib.optionals isLinux [
                "x86_64-unknown-linux-musl"
                "aarch64-unknown-linux-musl"
                "arm-unknown-linux-musleabihf"
                "armv7-unknown-linux-musleabihf"
                "x86_64-pc-windows-gnu"
                "x86_64-unknown-freebsd"
              ]
              ++ pkgs.lib.optionals pkgs.stdenv.isDarwin [
                "x86_64-apple-darwin"
                "aarch64-apple-darwin"
              ];
          };

          # Cross-compilation C toolchains from nixpkgs (Linux-only).
          x86_64MuslCC = pkgs.pkgsCross.musl64.stdenv.cc;
          aarch64MuslCC = pkgs.pkgsCross.aarch64-multiplatform-musl.stdenv.cc;
          armv6MuslCC = pkgs.pkgsCross.muslpi.stdenv.cc;
          mingw64CC = pkgs.pkgsCross.mingwW64.stdenv.cc;
          mingw64Mcfgthreads = pkgs.pkgsCross.mingwW64.windows.mcfgthreads;

          # BSD cross-compilers (Linux-only, amd64 only).
          freebsdCC = pkgs.pkgsCross.x86_64-freebsd.stdenv.cc;

          # armv7l-musl has no predefined pkgsCross entry.
          armv7lMuslPkgs = import nixpkgs {
            localSystem = system;
            crossSystem = { config = "armv7l-unknown-linux-musleabihf"; };
          };
          armv7lMuslCC = armv7lMuslPkgs.stdenv.cc;

          # Wrapper scripts mapping musl.cc-style names (used by release-with-wallet.sh
          # and install-musl-deps.sh) to the Nix cross-compiler binaries.
          # extraFlags is prepended before the caller's arguments (e.g. -L paths).
          mkWrapper = name: cc: binName: extraFlags:
            pkgs.writeShellScriptBin name ''exec ${cc}/bin/${binName} ${extraFlags} "$@"'';

          crossWrappers = pkgs.symlinkJoin {
            name = "cross-compiler-wrappers";
            paths = [
              (mkWrapper "x86_64-linux-musl-gcc" x86_64MuslCC "x86_64-unknown-linux-musl-gcc" "")
              (mkWrapper "aarch64-linux-musl-gcc" aarch64MuslCC "aarch64-unknown-linux-musl-gcc" "")
              (mkWrapper "armv6-linux-musleabihf-gcc" armv6MuslCC "armv6l-unknown-linux-musleabihf-gcc" "")
              (mkWrapper "armv7l-linux-musleabihf-gcc" armv7lMuslCC "armv7l-unknown-linux-musleabihf-gcc" "")
              # Nix's MinGW uses MCF threading; pass -L so the linker finds libmcfgthread.
              (mkWrapper "x86_64-w64-mingw32-gcc" mingw64CC "x86_64-w64-mingw32-gcc"
                "-L${mingw64Mcfgthreads}/lib")
              # BSD cross-compilers (amd64 only).
              (mkWrapper "x86_64-freebsd-cc" freebsdCC "x86_64-unknown-freebsd-clang" "")
            ];
          };
        in
        {
          default = pkgs.mkShell {
            nativeBuildInputs = [ pkgs.pkg-config ];

            buildInputs = [
              pkgs.go
              pkgs.gopls
              pkgs.graphviz
              pkgs.golangci-lint

              rustToolchain
            ] ++ pkgs.lib.optionals isLinux [
              crossWrappers
            ];

            shellHook = ''
              export GOPATH=$PWD/.go
              export PATH=$PWD/.go/bin:$PATH
              export GOENV=$PWD/.go/env

              export CARGO_HOME=$PWD/.cargo
              export PATH=$PWD/.cargo/bin:$PATH
            '';
          };
        }
      );
    };
}
