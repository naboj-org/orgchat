# syntax=docker/dockerfile:1

FROM node:24-alpine AS frontend

WORKDIR /src/frontend

RUN npm install --global pnpm@10.11.0

COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN --mount=type=cache,id=orgchat-pnpm,target=/pnpm/store \
    pnpm install --frozen-lockfile --store-dir=/pnpm/store

COPY frontend/ ./
RUN pnpm build

FROM golang:1.27-alpine AS backend

WORKDIR /src/server

COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY server/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/orgchat .

FROM alpine:3.23 AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 orgchat \
    && adduser -S -D -H -u 10001 -G orgchat orgchat

WORKDIR /app

COPY --from=backend /out/orgchat /usr/local/bin/orgchat
COPY --from=frontend /src/frontend/dist/ /app/public/

ENV HOST=0.0.0.0 \
    PORT=3001 \
    STATIC_DIR=/app/public \
    COOKIE_SECURE=true

USER 10001:10001

EXPOSE 3001

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q --spider "http://127.0.0.1:${PORT}/healthz" || exit 1

ENTRYPOINT ["/usr/local/bin/orgchat"]
