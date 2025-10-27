# syntax=docker/dockerfile:1.6

ARG GO_VERSION=1.24

FROM --platform=${BUILDPLATFORM} golang:${GO_VERSION}-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
WORKDIR /workspace

# Install build deps
RUN apk add --no-cache ca-certificates git

# Copy modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build the manager binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /workspace/bin/rebalancer ./cmd/manager

# Minimal runtime image
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=builder /workspace/bin/rebalancer /rebalancer

USER nonroot:nonroot
ENTRYPOINT ["/rebalancer"]
