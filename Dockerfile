# Standalone application image. No Enterprise source code is copied or linked.
FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package.json web/tsconfig.json web/vite.config.ts web/index.html ./
COPY web/src ./src
RUN npm install --no-audit --no-fund && npm run build

FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN go mod tidy && CGO_ENABLED=0 go build -tags timetzdata -trimpath -ldflags="-s -w" -o /photobooth ./cmd/photobooth

FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata && \
    test -s /usr/share/zoneinfo/Asia/Ho_Chi_Minh && \
    adduser -D -u 10001 photobooth
WORKDIR /app
RUN mkdir -p /var/lib/apexvoid/bootstrap && chown -R photobooth:photobooth /var/lib/apexvoid
COPY --from=backend /photobooth /app/photobooth
COPY --from=frontend /web/dist /app/web/dist
COPY db/migrations /app/db/migrations
USER photobooth
EXPOSE 8090
ENV LISTEN_ADDR=:8090 WEB_DIST=web/dist SCHEMA_DIR=db/migrations APEXVOID_APP_VERSION=0.2.0 APEXVOID_MIGRATION_BUNDLE_VERSION=0.2.0
ENV BOOTSTRAP_STATE_DIR=/var/lib/apexvoid/bootstrap DATABASE_HOST=postgres DATABASE_PORT=5432 DATABASE_SSLMODE=disable SERVICE_URL=http://photobooth:8090
HEALTHCHECK --interval=20s --timeout=3s --start-period=10s CMD wget -q -O /dev/null http://127.0.0.1:8090/health || exit 1
ENTRYPOINT ["/app/photobooth"]
