#!/bin/bash

cargo build --release && uniffi-bindgen-go -o bdkwallet bdkwallet/bdkgo_crate/src/bdkgo.udl
