FROM golang:1.26-alpine AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /gateway ./cmd/gateway
RUN CGO_ENABLED=0 go build -o /healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static AS runtime
COPY --from=builder /gateway /gateway
COPY --from=builder /healthcheck /healthcheck
EXPOSE 8008 8080
ENTRYPOINT ["/gateway"]
