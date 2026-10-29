# syntax=docker/dockerfile:1

FROM node:22-bookworm-slim AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-bookworm AS server-build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/paper-collect ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/paper-collect-migrate ./cmd/migrate

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata wget libreoffice-writer fonts-noto-cjk \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system app \
    && useradd --system --gid app --home-dir /app --no-create-home app \
    && mkdir -p /app/data /app/web \
    && chown -R app:app /app

COPY --from=server-build /out/paper-collect /app/paper-collect
COPY --from=server-build /out/paper-collect-migrate /app/paper-collect-migrate
COPY --from=web-build /src/web/dist/ /app/web/

WORKDIR /app
USER app
EXPOSE 8080
VOLUME ["/app/data"]

ENTRYPOINT ["/app/paper-collect"]
