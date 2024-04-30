utreexod
====

[![Build Status](https://github.com/utreexo/utreexod/workflows/Build%20and%20Test/badge.svg)](https://github.com/utreexo/utreexod/actions)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)
[![GoDoc](https://img.shields.io/badge/godoc-reference-blue.svg)](https://godoc.org/github.com/utreexo/utreexod)

utreexod is a full node bitcoin implementation with support for utreexo accumulators. Utreexo accumulator is
an append only merkle forest data structure with support for deleting elements from the set. More information at
[Utreexo library github repository](https://github.com/utreexo/utreexo)

The main features over a traditional node are:

- Immediate node bootstrap by having the UTXO state hardcoded into the codebase.
- Uses a tiny amount of memory.
- Extremely low disk i/o needed compared to a traditional node. Will not wear out micro sd cards.

The catch is that it uses more bandwidth compared to a normal node. For block downloads it's around 1.7
times more data. For transactions the absolute worst case is 4 times more but transaction relay supports
caching so it'll be a lot better.

This project is currently under active development and is in a beta state. Using it on mainnet for anything
other than non-negligible amounts of Bitcoin is not recommended.

## Requirements

[Go](http://golang.org) 1.18 or newer.

[Rust](http://rust-lang.org) 1.73.0 or newer (To compile the built in [BDK wallet](https://bitcoindevkit.org) support).

## Installation

https://github.com/utreexo/utreexod/

#### Linux/MacOSX - Build from Source

To build with BDK wallet:

```bash
make all
```

To build without the BDK wallet:

```bash
go build -o . ./...
```

## Getting Started

To run a utreexo node:

```bash
# The node will start from the hardcoded UTXO state and skip the initial block download.
# If the node was built with the bdk wallet, it'll start with the bdk wallet enabled.
`./utreexod`

# For utreexo archival nodes that will not skip the initial block download.
`./utreexod --noassumeutreexo --prune=0`

# To disable to bdkwallet. NOTE: the wallet will not be disabled if the node had ever
# started up with the wallet enabled.
`./utreexod --nobdkwallet`
```

To use the built in bdk wallet:

```bash
# Show the mnemonic word list of the wallet.
`./utreexoctl getmnemonicwords`

# Get a fresh address from the wallet.
`./utreexoctl freshaddress`

# Get an address that has not received funds yet from the wallet.
`./utreexoctl unusedaddress`

# Get an address at the desired index.
`./utreexoctl peekaddress "bip32-index"`
Example:
# Returns the address at index 100.
`./utreexoctl peekaddress 100`

# Get the current balance of the wallet.
`./utreexoctl balance`

# List all the relevant transactions for the wallet.
`./utreexoctl listbdktransactions`

# List all the relevant utxos that the wallet controls.
`./utreexoctl listbdkutxos`

# Create a transaction from the wallet.
`./utreexoctl createtransactionfrombdkwallet "feerate_in_sat_per_vbyte" [{"amount":n,"address":"value"},...]`
Example:
# feerate of 1 satoshi per vbyte, sending 10,000sats to address tb1pdt9hl8ymdetdmvgk54aft8jaq4xle998m8e6adwxs4vh7vwpl9jsyadlhq
`./utreexoctl createtransactionfrombdkwallet 1 '[{"amount":10000,"address":"tb1pdt9hl8ymdetdmvgk54aft8jaq4xle998m8e6adwxs4vh7vwpl9jsyadlhq"}]'`
# feerate of 12 satoshi per vbyte, sending 10,000sats to address tb1pdt9hl8ymdetdmvgk54aft8jaq4xle998m8e6adwxs4vh7vwpl9jsyadlhq and 20,000sats to address tb1puuv30z568uc58c40duwl5ytyu5898fyehlyqtm0al2xk70z8tw0qcxfn6w
`./utreexoctl createtransactionfrombdkwallet 12 '[{"amount":10000,"address":"tb1pdt9hl8ymdetdmvgk54aft8jaq4xle998m8e6adwxs4vh7vwpl9jsyadlhq"},{"amount":20000,"address":"tb1puuv30z568uc58c40duwl5ytyu5898fyehlyqtm0al2xk70z8tw0qcxfn6w"}]'`
```

Bridge nodes are nodes that keep the entire merkle forest and attach proofs to new blocks
and transactions. Since miners and nodes publish blocks and transactions without proofs, these
nodes are needed to allow for utreexo nodes without a soft fork. To run a bridge node:

```bash
# Either one will work. The only difference these have is that the flatutreexoproofindex
# stores all the data in .dat files instead of leveldb. It makes it easier to read the
# proofs for tinkering.
`./utreexod --flatutreexoproofindex`
`./utreexod --utreexoproofindex`

# For running bridge nodes that are also archival.
`./utreexod --flatutreexoproofindex --prune=0`
`./utreexod --utreexoproofindex --prune=0`
```

## Community

[Discord](https://discord.gg/6jRY2hqf3G)

## Issue Tracker

The [integrated github issue tracker](https://github.com/utreexo/utreexod/issues)
is used for this project.

## License

utreexod is licensed under the [copyfree](http://copyfree.org) ISC License.
