FROM golang:1.26-alpine AS builder
WORKDIR /workspace
RUN apk add -q --no-cache curl
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p internal/admin/static/vendor && \
    [ -f internal/admin/static/vendor/vue.esm-browser.prod.js ] || \
    curl -fsSL -o internal/admin/static/vendor/vue.esm-browser.prod.js \
        'https://cdn.jsdelivr.net/npm/vue@3.5.13/dist/vue.esm-browser.prod.js'
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
ARG RELEASE_VERSION=unknown
RUN go build \
    -ldflags "-X main.buildVersion=${RELEASE_VERSION} -X main.gitCommit=${GIT_COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /gateway ./cmd/gateway
RUN CGO_ENABLED=0 go build -o /healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static AS runtime
COPY --from=builder /gateway /gateway
COPY --from=builder /healthcheck /healthcheck
EXPOSE 8008 8080
ENTRYPOINT ["/gateway"]
