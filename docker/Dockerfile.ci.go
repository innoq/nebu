FROM golang:1.26-alpine

# Pre-install C toolchain required for the Go race detector (TSan runtime).
# Without gcc + musl-dev, `go test -race` fails on Alpine.
RUN apk add --no-cache gcc musl-dev git curl bash python3 nodejs npm

WORKDIR /workspace
