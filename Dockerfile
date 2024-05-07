ARG ARCH
ARG VER_ALPINE=3.12

FROM golang:1.20.2-alpine3.16@sha256:1d7fec2b8f451917a45b3e41249d1b5474c5269cf03f5cbdbd788347a1f1237c AS build-container

ENV GO111MODULE=on

ADD . /app
WORKDIR /app
RUN go install -v . ./cmd/...

FROM ${ARCH:+${ARCH}/}alpine:${VER_ALPINE}

COPY --from=build-container /go/bin/utreexod /bin

VOLUME ["/root/.utreexod"]

# P2P network (mainnet, testnet, signet & regnet respectively)
EXPOSE 8333 18333 38333 18444

CMD ["utreexod", "--utreexoproofindex"]
