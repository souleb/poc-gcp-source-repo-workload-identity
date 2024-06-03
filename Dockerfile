ARG BASE_VARIANT=alpine
ARG GO_VERSION=1.22
ARG XX_VERSION=1.4.0

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-${BASE_VARIANT} as gostable

FROM gostable AS go-linux

# Build-base consists of build platform dependencies and xx.
# These will be used at current arch to yield execute the cross compilations.
FROM go-${TARGETOS} AS build-base

COPY --from=xx / /

# build-go-mod can still be cached at build platform architecture.
FROM build-base as build

ARG TARGETPLATFORM

# Configure workspace
WORKDIR /workspace

# Copy modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Cache modules
RUN go mod download

# Copy source code
COPY main.go main.go

ARG TARGETPLATFORM
ARG TARGETARCH

RUN xx-go build \
  -ldflags "-s -w" \
  -tags 'netgo,osusergo,static_build' \
  -o /controller -trimpath main.go;

# Ensure that the binary was cross-compiled correctly to the target platform.
RUN xx-verify --static /controller

FROM alpine:3.19

ARG TARGETPLATFORM
RUN apk --no-cache add ca-certificates \
  && update-ca-certificates

# Copy over binary from build
COPY --from=build /controller /usr/local/bin/

USER 65534:65534
ENTRYPOINT [ "controller" ]
