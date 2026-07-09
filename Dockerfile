# syntax=docker/dockerfile:1

# ---- build stage ----
# Build on the native BUILDPLATFORM and cross-compile to the target arch. This
# avoids QEMU emulation AND — critically for multi-arch — guarantees every target
# image gets a binary for ITS architecture instead of a cache-poisoned amd64 one.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
WORKDIR /workspace

# Cache module downloads separately from the source.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# TARGETOS/TARGETARCH are injected by buildx per target platform. Deliberately
# NO defaults: if they are ever missing, `go build` must fail loudly rather than
# silently produce an amd64 binary for an arm64 image.
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-${TARGETARCH} \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /webhook ./cmd/webhook

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /webhook /webhook
USER 65534:65534
ENTRYPOINT ["/webhook"]
