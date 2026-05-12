# Story 12.3 — Media Gateway Dockerfile
# Multi-stage build: builder compiles the Go binary; final image is minimal Alpine.
# The media gateway serves POST /_matrix/media/v3/upload and GET /_matrix/media/v3/download/{server}/{id}.

FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /media ./cmd/media

FROM alpine:3.19
RUN apk add --no-cache ca-certificates curl
COPY --from=builder /media /media
EXPOSE 8009
ENTRYPOINT ["/media"]
