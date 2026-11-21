# One image: a static Go binary serving the API under /api and the embedded front end at /.

FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY frontend ./frontend
# GOAMD64=v1: the production VPS CPU has no SSE4 / x86-64-v2
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="loot-planner" \
      org.opencontainers.image.description="Лут-планировщик: unofficial market analytics for blizzless.trade from public listings; not affiliated with Blizzless or Blizzard Entertainment"
COPY --from=build /out/server /app/server
COPY --from=build --chown=10001:10001 /out/data /data
ENV DATA_DIR=/data PORT=8000
USER 10001:10001
VOLUME /data
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["/app/server", "-healthcheck"]
# a single instance: the market lives in memory and is refreshed by a background goroutine
ENTRYPOINT ["/app/server"]
