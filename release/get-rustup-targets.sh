#!/bin/bash

rustup target add aarch64-unknown-linux-musl
rustup target add x86_64-unknown-linux-musl
rustup target add arm-unknown-linux-musleabihf
rustup target add armv7-unknown-linux-musleabihf

rustup target add aarch64-apple-darwin
rustup target add x86_64-apple-darwin

rustup target add x86_64-pc-windows-gnu
