utreexod
====

[![Build Status](https://github.com/utreexo/utreexod/workflows/Build%20and%20Test/badge.svg)](https://github.com/utreexo/utreexod/actions)
[![Coverage Status](https://coveralls.io/repos/github/utreexo/utreexod/badge.svg?branch=master)](https://coveralls.io/github/utreexo/utreexod?branch=main)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)
[![GoDoc](https://img.shields.io/badge/godoc-reference-blue.svg)](https://godoc.org/github.com/utreexo/utreexod)

utreexod is a full node bitcoin implementation written in Go to showcase utreexo accumulators.

This project is currently under active development and is in a beta state. Using it for anything other
than development and testing is not recommended.


## Requirements

[Go](http://golang.org) 1.17 or newer.

## Installation

https://github.com/utreexo/utreexod/

#### Linux/BSD/MacOSX/POSIX - Build from Source

- Install Go according to the installation instructions here:
  http://golang.org/doc/install

- Ensure Go was installed properly and is a supported version:

```bash
$ go version
$ go env GOROOT GOPATH
```

NOTE: The `GOROOT` and `GOPATH` above must not be the same path.  It is
recommended that `GOPATH` is set to a directory in your home directory such as
`~/goprojects` to avoid write permission issues.  It is also recommended to add
`$GOPATH/bin` to your `PATH` at this point.

- Run the following commands to obtain utreexod, all dependencies, and install it:

```bash
$ cd $GOPATH/src/github.com/utreexo/utreexod
$ go install -v . ./cmd/...
```

- utreexod (and utilities) will now be installed in ```$GOPATH/bin```.  If you did
  not already add the bin directory to your system path during Go installation,
  we recommend you do so now.

## Getting Started

As there aren't any utreexo bridge nodes up and running for utreexo nodes to
connect to, you must run your own bridge node.


```bash
$ ./utreexod --flatutreexoproofindex
```

Then you can connect your utreexo node to the bridge node.

```bash
$ ./utreexod --addpeer=ip_of_the_bridge_node
```

## Updating

#### Linux/BSD/MacOSX/POSIX - Build from Source

- Run the following commands to update btcd, all dependencies, and install it:

```bash
$ cd $GOPATH/src/github.com/utreexo/utreexod
$ git pull
$ go install -v . ./cmd/...
```

## IRC

- irc.libera.chat
- channel #utreexo
- [webchat](https://web.libera.chat/gamja/?channels=utreexo)

## Issue Tracker

The [integrated github issue tracker](https://github.com/utreexo/utreexod/issues)
is used for this project.

## License

utreexod is licensed under the [copyfree](http://copyfree.org) ISC License.
