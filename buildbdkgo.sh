#!/bin/bash

cargo install uniffi-bindgen-go --git https://github.com/NordSecurity/uniffi-bindgen-go --tag v0.2.0+v0.25.0 && \
cargo build --release && \
uniffi-bindgen-go -o bdkwallet bdkwallet/bdkgo_crate/src/bdkgo.udl
