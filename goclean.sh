#!/bin/bash
# The script does automatic checking on a Go package and its sub-packages, including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 3. go vet        (http://golang.org/cmd/vet)
# 4. gosimple      (https://github.com/dominikh/go-simple)
# 5. unconvert     (https://github.com/mdempsky/unconvert)

set -ex

go test -short -race -tags="rpctest" -tags="bdkwallet" ./...

# Automatic checks
golangci-lint run --disable-all \
--enable=gofmt \
--enable=govet \
--enable=gosimple \
--enable=unconvert \
--exclude-dirs=bdkwallet/bdkgo # these are generated files
