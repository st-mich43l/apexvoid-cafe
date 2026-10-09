# Standalone application image. No Enterprise source code is copied or linked.
FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package.json web/tsconfig.json web/vite.config.ts web/index.html ./
COPY web/src ./src
RUN npm install --no-audit --no-fund && npm run build

FROM golang:1.23-alpine AS backend
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN go mod tidy && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /cafe ./cmd/cafe

FROM alpine:3.21
RUN apk --no-cache add ca-certificates && adduser -D -u 10001 cafe
WORKDIR /app
COPY --from=backend /cafe /app/cafe
COPY --from=frontend /web/dist /app/web/dist
COPY db/migrations /app/db/migrations
USER cafe
EXPOSE 8090
ENV LISTEN_ADDR=:8090 WEB_DIST=web/dist SCHEMA_PATH=db/migrations/001_cafe.sql
HEALTHCHECK --interval=20s --timeout=3s --start-period=10s CMD wget -q -O /dev/null http://127.0.0.1:8090/health || exit 1
ENTRYPOINT ["/app/cafe"]
