# core/Dockerfile
FROM elixir:1.19-alpine AS builder
WORKDIR /app
RUN mix local.hex --force && mix local.rebar --force
COPY mix.exs mix.lock ./
COPY apps/room_manager/mix.exs apps/room_manager/
COPY apps/session_manager/mix.exs apps/session_manager/
COPY apps/presence/mix.exs apps/presence/
COPY apps/event_dispatcher/mix.exs apps/event_dispatcher/
COPY apps/signature/mix.exs apps/signature/
COPY apps/permissions/mix.exs apps/permissions/
RUN mix deps.get --only prod
COPY . .
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
ARG RELEASE_VERSION=unknown
ENV GIT_COMMIT=${GIT_COMMIT}
ENV BUILD_TIME=${BUILD_TIME}
ENV RELEASE_VERSION=${RELEASE_VERSION}
RUN MIX_ENV=prod mix release

FROM alpine:3.23 AS runtime
RUN apk add --no-cache libstdc++ openssl ncurses curl
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
ARG RELEASE_VERSION=unknown
ENV GIT_COMMIT=${GIT_COMMIT}
ENV BUILD_TIME=${BUILD_TIME}
ENV RELEASE_VERSION=${RELEASE_VERSION}
COPY --from=builder /app/_build/prod/rel/nebu ./
ENTRYPOINT ["./bin/nebu", "start"]
