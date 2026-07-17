# syntax=docker/dockerfile:1

# Multi-stage build for the Veridian fork of Sablier with the HashiCorp Nomad
# provider. Builds a static, CGO-free binary from ./cmd/sablier and ships it in
# a distroless image.
#
#   docker build -t ghcr.io/christ-roy/veridian-sablier:nomad .
#
# The Veridian custom theme is mounted at RUNTIME by the Nomad job, NOT baked
# into this image. The upstream themes (pkg/theme/embedded/*.html) are already
# compiled into the binary via go:embed, so nothing theme-related is COPYied.

########################################
# Stage 1 — build
########################################
FROM golang:1.26-alpine AS build

# git is needed by `go build` for VCS stamping; ca-certificates for module fetch.
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Prime the module cache first for better layer caching.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Now the rest of the source.
COPY . .

# Build metadata injected into pkg/version (mirrors .goreleaser.yaml ldflags).
ARG VERSION=nomad
ARG REVISION=dev
ARG BRANCH=feat/nomad-provider
ARG BUILD_DATE=unknown

ENV CGO_ENABLED=0 GOOS=linux

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath \
      -ldflags="-s -w \
        -X github.com/sablierapp/sablier/pkg/version.Version=${VERSION} \
        -X github.com/sablierapp/sablier/pkg/version.Revision=${REVISION} \
        -X github.com/sablierapp/sablier/pkg/version.Branch=${BRANCH} \
        -X github.com/sablierapp/sablier/pkg/version.BuildUser=docker \
        -X github.com/sablierapp/sablier/pkg/version.BuildDate=${BUILD_DATE}" \
      -o /sablier ./cmd/sablier

########################################
# Stage 2 — runtime
########################################
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="veridian-sablier" \
      org.opencontainers.image.description="Sablier fork with a HashiCorp Nomad provider (scale-to-zero on-demand)" \
      org.opencontainers.image.source="https://github.com/christ-roy/veridian-sablier" \
      org.opencontainers.image.vendor="Veridian"

# tzdata is compiled into the binary (time/tzdata), so no OS packages needed.
ENV TZ=UTC

COPY --from=build /sablier /sablier

EXPOSE 10000

ENTRYPOINT ["/sablier"]
CMD ["start"]
